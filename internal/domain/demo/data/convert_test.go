package data

import (
	"testing"
	"time"

	"gin-template/internal/domain/demo/biz"
)

// TestFromBizToBizRoundTrip 锁定 data 层的「领域模型 ↔ PO」映射是**完整**的：
// biz.Demo 的每个字段都必须被 fromBiz 写入 PO，并能被 toBiz 原样读回。
//
// 这一测试的用意是防止字段被静默丢弃：将来给 biz.Demo 加了字段而忘记改映射，
// 这里会因为结构体不等而失败（而不是等到线上发现某列没写进去）。
func TestFromBizToBizRoundTrip(t *testing.T) {
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	src := &biz.Demo{
		ID:          7,
		Name:        "名称",
		Description: "描述",
		Status:      1,
		CreatedBy:   42,
		CreatedAt:   created,
		UpdatedAt:   created.Add(time.Hour),
	}

	po := fromBiz(src)
	if got := po.toBiz(); *got != *src {
		t.Errorf("域模型往返转换后不一致：\n got = %+v\nwant = %+v", *got, *src)
	}
}

// TestFromBizLeavesSoftDeleteAlone 软删除状态属于仓储侧，不应由领域模型写入。
func TestFromBizLeavesSoftDeleteAlone(t *testing.T) {
	po := fromBiz(&biz.Demo{ID: 1})
	if po.DeletedAt.Valid || !po.DeletedAt.Time.IsZero() {
		t.Errorf("fromBiz 不应设置 deleted_at，实际：%+v", po.DeletedAt)
	}
}

// TestToBizMapsEveryColumn 反向：PO 的每个业务列都要出现在领域模型里。
func TestToBizMapsEveryColumn(t *testing.T) {
	created := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)

	po := &Demo{
		Name:        "n",
		Description: "d",
		Status:      1,
		CreatedBy:   9,
	}
	po.ID = 11
	po.CreatedAt = created
	po.UpdatedAt = created.Add(time.Minute)

	got := po.toBiz()
	want := &biz.Demo{
		ID:          11,
		Name:        "n",
		Description: "d",
		Status:      1,
		CreatedBy:   9,
		CreatedAt:   created,
		UpdatedAt:   created.Add(time.Minute),
	}
	if *got != *want {
		t.Errorf("PO → 领域模型不一致：\n got = %+v\nwant = %+v", *got, *want)
	}
}

// TestDemoTableName 固定表名，避免有人改类型名导致换表。
func TestDemoTableName(t *testing.T) {
	po := Demo{}
	if got := po.TableName(); got != "demo" {
		t.Errorf("TableName() = %q, want demo", got)
	}
}
