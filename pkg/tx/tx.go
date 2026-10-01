// Package tx 提供服务内事务的统一管理。
//
// 设计思路：
//
// 在 Kratos 分层架构（biz → data）中，单次请求可能调用多个 Repository，
// 而这些 Repository 的写入需要处于同一事务中。本包通过 context 传递 *sql.Tx，
// 使得上层（biz）可控制事务边界，下层（data/Repository）自动感知并复用该事务。
//
// 为什么存 *sql.Tx 而非 *gorm.DB？
//
//  1. *sql.Tx 是 database/sql 标准库的事务句柄，与框架解耦。
//  2. GORM 的 *gorm.DB 是链式调用对象，同一个实例在多次操作后其内部状态
//     （Statement、Error 等）会残留。Repo 间共享 *gorm.DB 极易导致
//     前一次操作的 Condition/Error 污染后续调用。
//  3. GORM Session 的 PrepareStmt 模式会缓存预编译语句到连接，
//     事务中连接被回收后缓存失效，直接报错。存 *sql.Tx 可以每次创建
//     全新 GORM Session（PrepareStmt: false）规避此问题。
//
// 使用示例：
//
//	// === biz 层：控制事务边界 ===
//	type SomeUseCase struct {
//	    txManager  TxManager
//	    orderRepo  OrderRepo
//	    ledgerRepo LedgerRepo
//	}
//
//	func (uc *SomeUseCase) PlaceOrder(ctx context.Context) error {
//	    return uc.txManager.WithTx(ctx, func(txCtx context.Context) error {
//	        if err := uc.orderRepo.Create(txCtx, order); err != nil {
//	            return err // 自动 Rollback
//	        }
//	        return uc.ledgerRepo.Append(txCtx, entry) // 自动 Commit
//	    })
//	}
//
//	// === data 层：透明复用父事务 ===
//	type orderRepo struct {
//	    db *gorm.DB
//	}
//
//	func (r *orderRepo) Create(ctx context.Context, o *Order) error {
//	    // WithContext 自动检测 ctx 中是否有事务
//	    return tx.WithContext(ctx, r.db).Create(o).Error
//	}
package tx

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/go-kratos/kratos/v3/log"
	"gorm.io/gorm"
)

// ctxKey 为不可导出类型，防止其他包写入同名字符串造成 context key 冲突。
type ctxKey struct{}

var txKey = ctxKey{}

// setTx 将 *sql.Tx 注入 context，供后续 Repository 调用 WithContext 时取出。
func setTx(ctx context.Context, sqlTx *sql.Tx) context.Context {
	return context.WithValue(ctx, txKey, sqlTx)
}

// GetTx 从 context 中取出当前活跃的数据库事务。
// 返回值可能为 nil（表示当前不在事务上下文中）。
func GetTx(ctx context.Context) *sql.Tx {
	tx, ok := ctx.Value(txKey).(*sql.Tx)
	if !ok {
		return nil
	}
	return tx
}

// dbFromSqlTx 以 *sql.Tx 为新连接池，创建独立的 GORM Session。
//
// 参数说明：
//   - NewDB: true
//     创建全新的 Statement 实例，不共享父 Session 的 Condition/Error/Clauses 等状态，
//     避免跨 Repo 调用时的会话污染。
//   - PrepareStmt: false
//     禁用预编译语句缓存。预编译语句绑定到数据库连接，事务中的连接在 Commit/Rollback
//     后归还连接池，下次复用该连接时缓存的语句已失效，会导致 "bad connection" 错误。
func dbFromSqlTx(db *gorm.DB, sqlTx *sql.Tx, ctx context.Context) *gorm.DB {
	session := db.Session(&gorm.Session{
		NewDB:   true,
		Context: ctx,
	})
	session.Statement.ConnPool = sqlTx
	return session
}

// WithContext 返回一个与 ctx 绑定的 *gorm.DB。
//
// 行为：
//   - 若 ctx 中存在事务（由 TxManager.WithTx 注入），返回绑定该事务的 Session。
//   - 若 ctx 中无事务，返回普通的 DB 连接（附带 WithContext 以传递 tracing/logger 等上下文）。
//
// 此函数应作为 Repository 中所有数据库操作的统一入口，
// 而不是直接引用 d.db 或 d.db.WithContext(ctx)。
func WithContext(ctx context.Context, db *gorm.DB) *gorm.DB {
	sqlTx := GetTx(ctx)
	if sqlTx == nil {
		return db.WithContext(ctx)
	}
	return dbFromSqlTx(db, sqlTx, ctx)
}

// TxManager 定义事务编排接口，供 biz 层使用。
//
// biz 层不应直接依赖 *gorm.DB，而是通过本接口开启事务，
// 保持业务逻辑与 ORM 框架解耦。
type TxManager interface {
	// WithTx 在数据库事务中执行 fn。
	//
	// 约定：
	//   - 若 ctx 中已存在事务（嵌套调用），直接执行 fn，复用外层事务。
	//   - 若 ctx 中不存在事务，创建新事务。
	//   - fn 返回 nil → 自动 Commit。
	//   - fn 返回 error → 自动 Rollback。
	WithTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// txManager 是 TxManager 的 GORM 实现。
type txManager struct {
	db *gorm.DB
}

// WithTx 在数据库事务中执行 fn。
//
// 约定：
//   - 若 ctx 中已存在事务（嵌套调用），创建 SAVEPOINT 执行 fn：
//     fn 返回 nil → 释放 SAVEPOINT（外层可继续）
//     fn 返回 error → ROLLBACK TO SAVEPOINT（只回滚嵌套部分，外层不受影响）
//   - 若 ctx 中不存在事务，创建新事务：
//     fn 返回 nil → 自动 Commit。
//     fn 返回 error → 自动 Rollback。
//     fn 内部 panic → GORM Transaction 方法 recover 并 Rollback。
//   - fn 接收到的 ctx 已携带 *sql.Tx，可传递给 Repository 使用。
func (tm *txManager) WithTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if sqlTx := GetTx(ctx); sqlTx != nil {
		return dbFromSqlTx(tm.db, sqlTx, ctx).Transaction(func(tx *gorm.DB) error {
			return fn(ctx)
		})
	}

	return tm.db.WithContext(ctx).Transaction(func(gormTx *gorm.DB) error {
		txCtx, err := bindGormTransaction(ctx, gormTx)
		if err != nil {
			log.Error(err.Error())
			return err
		}
		return fn(txCtx)
	})
}

func bindGormTransaction(ctx context.Context, gormTx *gorm.DB) (context.Context, error) {
	if gormTx == nil || gormTx.Statement == nil {
		return nil, fmt.Errorf("tx: missing GORM transaction statement")
	}
	sqlTx, ok := gormTx.Statement.ConnPool.(*sql.Tx)
	if !ok || sqlTx == nil {
		return nil, fmt.Errorf("tx: transaction connection is %T, want *sql.Tx", gormTx.Statement.ConnPool)
	}
	return setTx(ctx, sqlTx), nil
}

// WithTxContext 将一个已有的 *sql.Tx 注入 context。
//
// 用于以下场景：
//   - DTM 子事务屏障（barrier.CallWithDB）返回了 *sql.Tx，
//     需要传递给 Repo 的 tx.WithContext 使用。
//   - 第三方事务管理器（非 GORM）需要与 pkg/tx 协作。
//
// 注入后，任意 Repo 调用 tx.WithContext(ctx, db) 都会自动绑定到这个事务。
func WithTxContext(ctx context.Context, sqlTx *sql.Tx) context.Context {
	return setTx(ctx, sqlTx)
}

// NewTxManager 创建 TxManager 实例。
//
// 参数 db 应为 *gorm.DB 的主实例（非 Session 副本），
// 因为 dbFromSqlTx 会对其调用 Session() 生成独立副本。
func NewTxManager(db *gorm.DB) TxManager {
	return &txManager{db: db}
}
