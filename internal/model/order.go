// 订单实体（对齐原 order.entity.ts：MEMBERSHIP/PRODUCT/SERVICE/ARTICLE 四类，
// 状态机 PENDING→PAID/CANCELLED/REFUNDED）。
package model

import (
	"time"
)

// 订单状态（对齐原 enum）。
const (
	OrderStatusPending   = "PENDING"
	OrderStatusPaid      = "PAID"
	OrderStatusCancelled = "CANCELLED"
	OrderStatusRefunded  = "REFUNDED"
)

// 订单类型。
const (
	OrderTypeMembership = "MEMBERSHIP"
	OrderTypeProduct    = "PRODUCT"
	OrderTypeService    = "SERVICE"
	OrderTypeArticle    = "ARTICLE"
)

// Order 订单。
type Order struct {
	BaseModel
	UserID         string `gorm:"type:char(36);index;comment:用户ID（买家）" json:"user_id"`
	AuthorID       string `gorm:"type:char(36);comment:作者ID（卖家）" json:"author_id"`
	ArticleID      string `gorm:"type:char(36);comment:文章ID（ARTICLE 类型订单）" json:"article_id"`
	OrderNo        string `gorm:"type:varchar(64);uniqueIndex;comment:订单号" json:"order_no"`
	Type           string `gorm:"type:varchar(20);comment:订单类型" json:"type"`
	Title          string `gorm:"type:varchar(255);comment:订单标题" json:"title"`
	Amount         float64 `gorm:"type:decimal(10,2);comment:订单金额（元）" json:"amount"`
	PaymentMethod  string  `gorm:"type:varchar(20);comment:支付方式" json:"payment_method"`
	PaymentOrderNo string  `gorm:"type:varchar(128);comment:第三方支付订单号" json:"payment_order_no"`
	Status         string  `gorm:"type:varchar(20);index;default:PENDING;comment:订单状态" json:"status"`
	PaidAt         *time.Time `gorm:"comment:支付时间" json:"paid_at"`
	Details        map[string]interface{} `gorm:"type:jsonb;serializer:json;comment:订单详情" json:"details"`
	Remark         string `gorm:"type:text;comment:备注" json:"remark"`
}

// TableName 表名。
func (Order) TableName() string { return "orders" }

// ArticleOrderView 文章表订单域局部视图（仅声明创建文章订单所需列；
// 权威 article 模型归 content 域，本结构不参与 AutoMigrate）。
type ArticleOrderView struct {
	ID            string  `gorm:"type:char(36);primaryKey;column:id" json:"id"`
	AuthorID      string  `gorm:"type:char(36);column:author_id" json:"author_id"`
	Title         string  `gorm:"type:varchar(255);column:title" json:"title"`
	RequirePayment bool   `gorm:"column:require_payment" json:"require_payment"`
	ViewPrice     float64 `gorm:"type:decimal(10,2);column:view_price" json:"view_price"`
}

// TableName 表名。
func (ArticleOrderView) TableName() string { return "articles" }

// OrderQuery 订单列表查询条件（用户侧与管理员侧共用）。
type OrderQuery struct {
	Page    int    `form:"page"`
	Limit   int    `form:"limit"`
	Status  string `form:"status"`
	Type    string `form:"type"`
	UserID  string `form:"user_id"`
	Keyword string `form:"keyword"` // 订单号模糊
}

// OrderModels 订单域迁移实体（migrations.Run 中追加）。
func OrderModels() []interface{} { return []interface{}{&Order{}} }
