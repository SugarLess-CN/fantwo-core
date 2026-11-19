# PicArt API 文档清单

> 数据源（唯一权威）：`tmp/picart/swagger.json`（OpenAPI 3.0.0，`info.title` = "PicArt API"，`info.description` = "PicArt 图片社区 API 文档"）。
> 本文档供 Go 重构对照使用，字段名与 swagger 原文完全一致（snake_case / camelCase 原样保留）。

## 1. 总览

| 项目 | 值 |
|---|---|
| 路径数 | 211 |
| 操作数 | 275 |
| 业务 tag 数 | 24（含 1 个杂项 tag「App」，1 个操作）|
| components.schemas | 74 个 |

### tag 操作数统计

| tag | 操作数 |
|---|---|
| 用户管理 | 46 |
| 文章管理 | 33 |
| 装饰品管理 | 21 |
| 信息管理 | 19 |
| 积分管理 | 16 |
| 系统配置 | 13 |
| 表情包管理 | 13 |
| 上传管理 | 10 |
| 合集管理 | 10 |
| 角色管理 | 10 |
| 订单管理 | 10 |
| 评论管理 | 10 |
| 标签管理 | 9 |
| 成就管理 | 8 |
| 支付管理 | 7 |
| 举报管理 | 6 |
| 内容审核 | 6 |
| 轮播管理 | 6 |
| 邀请管理 | 6 |
| 分类管理 | 5 |
| 权限管理 | 5 |
| 搜索管理 | 3 |
| 统计模块 | 2 |
| App | 1 |
| **合计** | **275** |

## 2. 全局约定

### servers / basePath
- `servers` 为空数组，且所有路径均不带前缀（如 `/user/list`、`/article`），swagger 层面未声明 basePath。源码级全局前缀（原项目 `api/v1`）不在本 swagger 内体现，Go 重构时以源码为准。

### 鉴权方案（securitySchemes）

- `bearer`：type=http, scheme=bearer, bearerFormat=JWT —— 即 **JWT Bearer**，通过 HTTP 头 `Authorization: Bearer <token>` 传递。
- 全局 `security` 未声明（文档未说明默认鉴权）；鉴权按操作级 `security` 标注判断，各端点表「鉴权」列标 `security` 表示该操作显式声明了 security，标 `—` 表示操作级未标注（可能实际靠全局守卫保护，swagger 未体现，Go 重构时需对照源码确认）。

### 统一响应包装结构

从典型 200 响应 schema 推断，响应统一包装为：

```json
{ "code": 0, "message": "success", "data": <业务数据> }
```

- `code`: number，0（或 200）表示成功；`message`: string；`data`: 业务载荷（对象 / 数组 / 分页结构）。
- 分页响应 `data` 内常见字段：`list`(array)、`total`(number)、`pageSize`、`page`（各端点略有差异，以各端点响应字段为准）。

### 常见错误响应

错误响应在各操作的 `responses` 中出现的状态码分布：401 ×84、404 ×48、400 ×46、403 ×38、409 ×3、302 ×1、429 ×1、410 ×1、500 ×1。
错误体的具体 schema 文档多数未给出，按统一包装结构推断为 `{code: 非零, message: 错误信息, data: null}`。

## 3. 按 tag 分节的端点明细

### 3.1 用户管理（46 个操作）

| 方法 | 路径 | operationId | 摘要 | 鉴权 | 请求参数/请求体 | 响应关键字段 |
|---|---|---|---|---|---|---|
| GET | `/user` | UserController_findAll | 获取用户列表 | `security` | page(query):number; limit(query):number; username(query):string*; keyword(query):string* | （文档未说明） |
| POST | `/user` | UserController_create | 创建用户 | `security` | [application/json] username:string* nickname:string password:string* email:string phone:string roleIds:array<number> wallet:number inviteCode:string verificationCode:string | （文档未说明） |
| GET | `/user/certification/admin/applications` | UserController_findApplications | 管理员：分页查询认证申请列表 | `security` | page(query):number; limit(query):number; status(query):string*; type(query):string* | （文档未说明） |
| PATCH | `/user/certification/admin/applications/{id}/audit` | UserController_audit | 管理员：审核认证申请（通过/拒绝） | `security` | id(path):number*; [application/json] status:string* reason:string | （文档未说明） |
| POST | `/user/certification/admin/applications/{id}/revoke` | UserController_revoke | 管理员：撤销已通过的认证 | `security` | id(path):number* | （文档未说明） |
| POST | `/user/certification/apply` | UserController_apply | 申请认证（官方/创作者） | `security` | [application/json] type:string* realName:string organizationName:string description:string slogan:string materials:array<string> | （文档未说明） |
| GET | `/user/certification/identity` | UserController_getMyIdentity | 获取当前用户的认证身份（官方/创作者/普通用户） | `security` | （无） | （文档未说明） |
| GET | `/user/certification/identity/{userId}` | UserController_getIdentityByUserId | 查询指定用户的认证身份（官方/创作者/普通用户） | `security` | userId(path):number* | （文档未说明） |
| GET | `/user/certification/status` | UserController_getMyCertification | 获取我当前最新的认证记录（含申请状态，仅供本人查看） | `security` | （无） | （文档未说明） |
| POST | `/user/commission/calculate` | UserController_calculateCommission | 计算抽成金额 | `security` | [application/json] amount:number* type:string* | （文档未说明） |
| GET | `/user/commission/config` | UserController_getUserCommissionConfig | 获取当前用户抽成配置 | `security` | （无） | （文档未说明） |
| POST | `/user/commission/config` | UserController_setUserCommissionConfig | 设置用户抽成配置 | `security` | [application/json] articleCommissionRate:number* membershipCommissionRate:number* productCommissionRate:number* serviceCommissionRate:number* enableCustomCommission:boolean* remark:string* | （文档未说明） |
| GET | `/user/config` | UserController_getUserConfig | 获取当前用户配置 | `security` | （无） | （文档未说明） |
| PATCH | `/user/config` | UserController_updateUserConfig | 更新当前用户配置 | `security` | [application/json] articleCommissionRate:number membershipCommissionRate:number productCommissionRate:number serviceCommissionRate:number enableCustomCommission:boolean enableSystemNotification:boolean enableCommentNotification:boolean enableLikeNotification:boolean enableFollowNotification:boolean enableMessageNotification:boolean enableOrderNotification:boolean enablePaymentNotification:boolean …:共12个字段 | （文档未说明） |
| PATCH | `/user/config/commission` | UserController_updateCommissionSettings | 更新用户抽成设置 | `security` | （无） | （文档未说明） |
| PATCH | `/user/config/notifications` | UserController_updateNotificationSettings | 更新用户通知设置 | `security` | [application/json] enableSystemNotification:boolean enableCommentNotification:boolean enableLikeNotification:boolean enableFollowNotification:boolean enableMessageNotification:boolean enableOrderNotification:boolean enablePaymentNotification:boolean enableInviteNotification:boolean enableEmailNotification:boolean enableSmsNotification:boolean enablePushNotification:boolean | （文档未说明） |
| POST | `/user/email/verification` | UserController_sendVerificationCode | 发送邮箱验证码（通用接口） | `security` | [application/json] email:string* type:string | （文档未说明） |
| POST | `/user/login` | UserController_login | 用户登录（支持用户名或邮箱） | `security` | [application/json] account:?* password:string* | （文档未说明） |
| POST | `/user/logout` | UserController_logout | 退出登录（单设备） | `security` | （无） | （文档未说明） |
| POST | `/user/membership/batch-check` | UserController_batchCheckMembershipStatus | 批量检查并更新所有用户的会员状态 | `security` | （无） | （文档未说明） |
| GET | `/user/oauth/github/callback` | UserController_githubOAuthRedirectCallback | GitHub OAuth 回调（重定向模式，成功后跳转前端页面） | `security` | code(query):string*; state(query):string*; error(query):string* | （文档未说明） |
| POST | `/user/oauth/github/callback` | UserController_githubOAuthCallback | GitHub OAuth 登录（SPA 模式，用 code 换取 token） | `security` | [application/json] code:string* state:string | （文档未说明） |
| GET | `/user/oauth/github/url` | UserController_getGithubOAuthUrl | 获取 GitHub OAuth 授权链接 | `security` | （无） | （文档未说明） |
| POST | `/user/password/change` | UserController_changePassword | 修改密码（需要原密码） | `security` | [application/json] oldPassword:string* newPassword:string* | （文档未说明） |
| POST | `/user/password/reset` | UserController_resetPassword | 重置密码 | `security` | （无） | （文档未说明） |
| GET | `/user/profile` | UserController_getProfile | 获取当前用户信息 | `security` | （无） | （文档未说明） |
| PATCH | `/user/profile/contact` | UserController_updateContact | 修改当前用户邮箱和手机号 | `security` | [application/json] email:string phone:string verificationCode:string | （文档未说明） |
| POST | `/user/refresh-token` | UserController_refreshToken | 刷新 access token | `security` | [application/json] refreshToken:string | （文档未说明） |
| POST | `/user/register` | UserController_registerUser | 用户注册 | `security` | [application/json] username:string* nickname:string password:string* email:string phone:string roleIds:array<number> wallet:number inviteCode:string verificationCode:string | （文档未说明） |
| POST | `/user/sign-in` | UserController_signIn | 手动签到 | `security` | （无） | （文档未说明） |
| GET | `/user/sign-in/records` | UserController_getSignInRecords | 获取签到记录 | `security` | days(query):number* | （文档未说明） |
| GET | `/user/sign-in/stats` | UserController_getSignInStats | 获取签到统计 | `security` | （无） | （文档未说明） |
| GET | `/user/wallet/balance` | UserController_getWalletBalance | 获取钱包余额 | `security` | （无） | （文档未说明） |
| GET | `/user/wallet/statistics` | UserController_getWalletStatistics | 获取钱包统计信息 | `security` | （无） | （文档未说明） |
| GET | `/user/wallet/transactions` | UserController_getWalletTransactions | 获取钱包交易记录 | `security` | page(query):number*; limit(query):number*; type(query):string* | （文档未说明） |
| POST | `/user/wallet/withdraw` | UserController_withdrawWallet | 钱包提现 | `security` | （无） | （文档未说明） |
| DELETE | `/user/{id}` | UserController_remove | 删除用户 | `security` | id(path):string* | （文档未说明） |
| GET | `/user/{id}` | UserController_findOne | 获取用户详情 | `security` | id(path):string* | （文档未说明） |
| PATCH | `/user/{id}` | UserController_update | 更新用户 | `security` | id(path):number*; [application/json] username:string nickname:string roleIds:array<number> wallet:number inviteCode:string avatar:string description:string background:string address:string gender:string birthDate:string membershipLevel:number …:共12个字段 | （文档未说明） |
| POST | `/user/{id}/follow` | UserController_follow | 关注用户 | `security` | id(path):string* | （文档未说明） |
| GET | `/user/{id}/followers` | UserController_getFollowers | 获取粉丝列表 | `security` | id(path):string*; page(query):number; limit(query):number; keyword(query):string* | （文档未说明） |
| GET | `/user/{id}/followers/count` | UserController_getFollowerCount | 获取粉丝数量 | `security` | id(path):string* | （文档未说明） |
| GET | `/user/{id}/followings` | UserController_getFollowings | 获取关注列表 | `security` | id(path):string*; page(query):number; limit(query):number; keyword(query):string* | （文档未说明） |
| GET | `/user/{id}/followings/count` | UserController_getFollowingCount | 获取关注数量 | `security` | id(path):string* | （文档未说明） |
| GET | `/user/{id}/membership/check` | UserController_checkMembershipStatus | 检查并更新用户会员状态 | `security` | id(path):string* | （文档未说明） |
| POST | `/user/{id}/unfollow` | UserController_unfollow | 取关用户 | `security` | id(path):string* | （文档未说明） |

### 3.2 文章管理（33 个操作）

| 方法 | 路径 | operationId | 摘要 | 鉴权 | 请求参数/请求体 | 响应关键字段 |
|---|---|---|---|---|---|---|
| GET | `/article` | ArticleController_findAll | 获取文章列表 | `security` | page(query):number; limit(query):number; title(query):string*; categoryId(query):number*; status(query):string; type(query):string*; tagId(query):number* | （文档未说明） |
| POST | `/article` | ArticleController_create | 创建文章 | `security` | [application/json] title:string* content:string* summary:string images:? videoUrl:string cover:string sort:number* categoryId:number* tagNames:array<string> tagIds:array<string> status:string requireLogin:boolean* …:共12个字段 | （文档未说明） |
| GET | `/article/author/{id}` | ArticleController_findByAuthor | 根据作者获取文章列表 | `security` | id(path):string*; page(query):number; limit(query):number; type(query):string*; categoryId(query):number*; keyword(query):string* | （文档未说明） |
| DELETE | `/article/browse` | ArticleController_clearBrowseHistory | 清空浏览历史 | `security` | （无） | （文档未说明） |
| POST | `/article/browse/batch-delete` | ArticleController_batchDeleteBrowseHistory | 批量删除浏览记录 | `security` | （无） | （文档未说明） |
| GET | `/article/browse/history` | ArticleController_getUserBrowseHistory | 获取用户浏览历史列表 | `security` | page(query):number; limit(query):number; startDate(query):string; endDate(query):string; categoryId(query):number; order(query):string(newest\|oldest) | （文档未说明） |
| GET | `/article/browse/recent` | ArticleController_getRecentBrowsedArticles | 获取最近浏览的文章 | `security` | limit(query):number* | （文档未说明） |
| GET | `/article/browse/stats` | ArticleController_getBrowseStats | 获取浏览统计 | `security` | （无） | （文档未说明） |
| DELETE | `/article/browse/{articleId}` | ArticleController_deleteBrowseHistory | 删除单条浏览记录 | `security` | articleId(path):string* | （文档未说明） |
| GET | `/article/browse/{articleId}` | ArticleController_getBrowseHistory | 获取单条浏览记录 | `security` | articleId(path):string* | （文档未说明） |
| GET | `/article/disliked/list` | ArticleController_getDislikedArticles | 获取当前用户不喜欢的文章列表 | `security` | page(query):number; limit(query):number | （文档未说明） |
| GET | `/article/drafts` | ArticleController_getDrafts | 获取当前用户的草稿文章列表 | `security` | page(query):number; limit(query):number | （文档未说明） |
| GET | `/article/favorited/list` | ArticleController_getFavoritedArticles | 获取用户收藏的文章列表 | `security` | page(query):number; limit(query):number; userId(query):number* | （文档未说明） |
| GET | `/article/hot-search` | ArticleController_getHotSearches | 获取热门搜索词 | `security` | limit(query):number; keyword(query):string | （文档未说明） |
| GET | `/article/liked` | ArticleController_getLikedArticles | 获取用户点赞文章列表 | `security` | page(query):number; limit(query):number | （文档未说明） |
| GET | `/article/published/ids` | ArticleController_getPublishedArticleIds | 获取已发布文章ID列表 | `security` | （无） | （文档未说明） |
| GET | `/article/recommend/{id}` | ArticleController_findRecommendations | 获取相关文章 | `security` | id(path):string* | （文档未说明） |
| GET | `/article/search` | ArticleController_search | 搜索文章 | `security` | keyword(query):string*; page(query):number; limit(query):number; categoryId(query):number*; sortBy(query):string(relevance\|latest\|views\|likes) | （文档未说明） |
| DELETE | `/article/{id}` | ArticleController_remove | 删除文章 | `security` | id(path):string* | （文档未说明） |
| GET | `/article/{id}` | ArticleController_findOne | 获取文章详情 | `security` | id(path):string* | （文档未说明） |
| PATCH | `/article/{id}` | ArticleController_update | 更新文章 | `security` | id(path):string*; [application/json] title:string content:string summary:string images:? videoUrl:string cover:string sort:number categoryId:number tagNames:array<string> tagIds:array<string> status:string requireLogin:boolean …:共12个字段 | （文档未说明） |
| POST | `/article/{id}/browse/progress` | ArticleController_updateBrowseProgress | 更新文章浏览进度 | `security` | id(path):string*; [application/json] progress:number duration:number | （文档未说明） |
| DELETE | `/article/{id}/dislike` | ArticleController_cancelDislikeArticle | 取消不喜欢这类内容 | `security` | id(path):string* | （文档未说明） |
| POST | `/article/{id}/dislike` | ArticleController_dislikeArticle | 标记不喜欢这类内容 | `security` | id(path):string*; [application/json] reason:string | （文档未说明） |
| GET | `/article/{id}/dislike/status` | ArticleController_getDislikeStatus | 获取不喜欢状态 | `security` | id(path):string* | （文档未说明） |
| DELETE | `/article/{id}/favorite` | ArticleController_unfavoriteArticle | 取消收藏文章 | `security` | id(path):string* | （文档未说明） |
| POST | `/article/{id}/favorite` | ArticleController_favoriteArticle | 收藏文章（添加到默认收藏夹） | `security` | id(path):string* | （文档未说明） |
| GET | `/article/{id}/favorite/status` | ArticleController_checkFavoriteStatus | 检查文章是否已收藏 | `security` | id(path):string* | （文档未说明） |
| PATCH | `/article/{id}/featured` | ArticleController_setFeatured | 设置文章精华状态 | `security` | id(path):string*; [application/json] isFeatured:boolean* | （文档未说明） |
| POST | `/article/{id}/like` | ArticleController_like | 点赞/表情回复文章 | `security` | id(path):string*; [application/json] reactionType:string* | （文档未说明） |
| GET | `/article/{id}/like/count` | ArticleController_getLikeCount | 获取文章点赞数量 | `security` | id(path):string* | （文档未说明） |
| GET | `/article/{id}/like/status` | ArticleController_getLikeStatus | 获取文章点赞状态 | `security` | id(path):string* | （文档未说明） |
| PATCH | `/article/{id}/profile-pin` | ArticleController_setProfilePin | 设置文章个人主页置顶状态 | `security` | id(path):string*; [application/json] isPinned:boolean* | （文档未说明） |

### 3.3 装饰品管理（21 个操作）

| 方法 | 路径 | operationId | 摘要 | 鉴权 | 请求参数/请求体 | 响应关键字段 |
|---|---|---|---|---|---|---|
| GET | `/decoration` | DecorationController_findAll | 获取装饰品列表 | `security` | type(query):string; status(query):string; keyword(query):string; page(query):number; limit(query):number; sortBy(query):string*; sortOrder(query):string* | （文档未说明） |
| POST | `/decoration` | DecorationController_create | 创建装饰品 | `security` | [application/json] name:string* type:string* description:string imageUrl:string* previewUrl:string rarity:string bubbleColor:string obtainMethod:string* isPurchasable:boolean price:number isPermanent:boolean validDays:number …:共12个字段 | （文档未说明） |
| GET | `/decoration/achievement-badges/my` | DecorationController_getMyAchievementBadges | 获取我的成就勋章 | `security` | page(query):number; limit(query):number | （文档未说明） |
| GET | `/decoration/activity` | DecorationController_findAllActivities | 获取活动列表 | `security` | status(query):string; type(query):string; page(query):number; limit(query):number | （文档未说明） |
| POST | `/decoration/activity` | DecorationController_createActivity | 创建活动 | `security` | [application/json] name:string* description:string type:string* decorationId:number* articleId:number requiredLikes:number requiredComments:number requiredShares:number requiredRecharge:number requiredSignInDays:number isPermanent:boolean* validDays:number …:共12个字段 | （文档未说明） |
| POST | `/decoration/activity/claim/{activityId}` | DecorationController_claimActivityReward | 领取活动奖励 | `security` | activityId(path):string* | （文档未说明） |
| GET | `/decoration/activity/progress/my` | DecorationController_getMyActivityProgress | 获取我的活动进度 | `security` | （无） | （文档未说明） |
| DELETE | `/decoration/activity/{id}` | DecorationController_removeActivity | 删除活动 | `security` | id(path):string* | （文档未说明） |
| GET | `/decoration/activity/{id}` | DecorationController_findOneActivity | 获取活动详情 | `security` | id(path):string* | （文档未说明） |
| PATCH | `/decoration/activity/{id}` | DecorationController_updateActivity | 更新活动 | `security` | id(path):string*; [application/json] name:string description:string type:string decorationId:number articleId:number requiredLikes:number requiredComments:number requiredShares:number requiredRecharge:number requiredSignInDays:number isPermanent:boolean validDays:number …:共12个字段 | （文档未说明） |
| POST | `/decoration/clean-expired` | DecorationController_cleanExpired | 清理过期装饰品 | `security` | （无） | （文档未说明） |
| POST | `/decoration/gift` | DecorationController_gift | 赠送装饰品 | `security` | [application/json] toUserId:number* decorationId:number* message:string | （文档未说明） |
| POST | `/decoration/purchase` | DecorationController_purchase | 购买装饰品 | `security` | [application/json] decorationId:number* | （文档未说明） |
| POST | `/decoration/unuse/{decorationId}` | DecorationController_unuseDecoration | 取消使用装饰品 | `security` | decorationId(path):string* | （文档未说明） |
| POST | `/decoration/use/{decorationId}` | DecorationController_useDecoration | 使用装饰品 | `security` | decorationId(path):string* | （文档未说明） |
| GET | `/decoration/user/current/decorations` | DecorationController_getCurrentDecorations | 获取当前使用的装饰品 | `security` | （无） | （文档未说明） |
| GET | `/decoration/user/my` | DecorationController_getMyDecorations | 获取我的装饰品 | `security` | type(query):string*; page(query):number; limit(query):number | （文档未说明） |
| GET | `/decoration/user/{userId}` | DecorationController_getUserDecorations | 获取用户的装饰品 | `security` | userId(path):string*; type(query):string*; page(query):number; limit(query):number | （文档未说明） |
| DELETE | `/decoration/{id}` | DecorationController_remove | 删除装饰品 | `security` | id(path):string* | （文档未说明） |
| GET | `/decoration/{id}` | DecorationController_findOne | 获取装饰品详情 | `security` | id(path):string* | （文档未说明） |
| PATCH | `/decoration/{id}` | DecorationController_update | 更新装饰品 | `security` | id(path):string*; [application/json] name:string type:string description:string imageUrl:string previewUrl:string rarity:string bubbleColor:string obtainMethod:string isPurchasable:boolean price:number isPermanent:boolean validDays:number …:共12个字段 | （文档未说明） |

### 3.4 信息管理（19 个操作）

| 方法 | 路径 | operationId | 摘要 | 鉴权 | 请求参数/请求体 | 响应关键字段 |
|---|---|---|---|---|---|---|
| GET | `/message` | MessageController_findAll | 获取当前用户所有消息（含全员通知） | `security` | page(query):number; limit(query):number | （文档未说明） |
| POST | `/message` | MessageController_create | 创建消息（支持全员、部分、个人通知） | `security` | [application/json] senderId:object receiverId:number receiverIds:array<string> content:string* title:string type:string* isBroadcast:boolean* metadata:object | （文档未说明） |
| POST | `/message/batch` | MessageController_batchOperation | 批量操作消息（标记已读/删除） | `security` | [application/json] messageIds:array<string>* action:string* | （文档未说明） |
| DELETE | `/message/private/block/{userId}` | MessageController_unblockPrivateUser | 取消拉黑私信对象 | `security` | userId(path):string* | （文档未说明） |
| POST | `/message/private/block/{userId}` | MessageController_blockPrivateUser | 拉黑私信对象 | `security` | userId(path):string* | （文档未说明） |
| GET | `/message/private/blocks` | MessageController_getBlockedUsers | 获取拉黑列表 | `security` | （无） | （文档未说明） |
| GET | `/message/private/conversations` | MessageController_getPrivateConversations | 获取当前用户的私信会话列表 | `security` | cursor(query):string; limit(query):number | （文档未说明） |
| GET | `/message/private/conversations/{userId}/messages` | MessageController_getPrivateConversation | 获取与指定用户的私信记录 | `security` | userId(path):string*; cursor(query):string; limit(query):number | （文档未说明） |
| POST | `/message/private/conversations/{userId}/read` | MessageController_markPrivateConversationRead | 标记与指定用户的整个私信会话为已读 | `security` | userId(path):string* | （文档未说明） |
| POST | `/message/private/read-batch` | MessageController_markPrivateMessagesRead | 批量标记私信已读 | `security` | [application/json] messageIds:array<string>* | （文档未说明） |
| POST | `/message/private/recall/{id}` | MessageController_recallPrivateMessage | 撤回私信 | `security` | id(path):string*; [application/json] reason:string | （文档未说明） |
| POST | `/message/private/{userId}` | MessageController_sendPrivateMessage | 发送私信 | `security` | userId(path):string*; [application/json] content:string messageKind:string* payload:object | （文档未说明） |
| POST | `/message/read-all` | MessageController_markAllAsRead | 标记所有消息为已读 | `security` | [application/json] type:string isBroadcast:boolean | （文档未说明） |
| GET | `/message/search` | MessageController_search | 高级查询消息 | `security` | page(query):number; limit(query):number; type(query):string(private\|system\|notification); isRead(query):boolean; isBroadcast(query):boolean; keyword(query):string; senderId(query):number; receiverId(query):number | （文档未说明） |
| GET | `/message/unread/count` | MessageController_getUnreadCount | 获取未读消息数量 | `security` | （无） | （文档未说明） |
| DELETE | `/message/{id}` | MessageController_remove | 删除消息 | `security` | id(path):string* | （文档未说明） |
| GET | `/message/{id}` | MessageController_findOne | 获取单条消息 | `security` | id(path):string* | （文档未说明） |
| PATCH | `/message/{id}` | MessageController_update | 更新消息内容 | `security` | id(path):string*; [application/json] content:string title:string isRead:boolean type:string isBroadcast:boolean metadata:object | （文档未说明） |
| POST | `/message/{id}/read` | MessageController_markAsRead | 标记消息为已读 | `security` | id(path):string* | （文档未说明） |

### 3.5 积分管理（16 个操作）

| 方法 | 路径 | operationId | 摘要 | 鉴权 | 请求参数/请求体 | 响应关键字段 |
|---|---|---|---|---|---|---|
| GET | `/points/activities` | PointsController_findAllActivities | 获取积分活动列表 | — | type(query):string*; keyword(query):string* | （文档未说明） |
| POST | `/points/activities` | PointsController_createActivity | 创建积分活动 | `security` | [application/json] code:string* name:string* description:string type:string* rewardPoints:number* targetCount:number dailyLimit:number totalLimit:number validDays:number icon:string link:string isActive:boolean …:共12个字段 | （文档未说明） |
| DELETE | `/points/activities/{id}` | PointsController_removeActivity | 删除积分活动 | `security` | id(path):string* | （文档未说明） |
| GET | `/points/activities/{id}` | PointsController_findOneActivity | 获取积分活动详情 | — | id(path):string* | （文档未说明） |
| PATCH | `/points/activities/{id}` | PointsController_updateActivity | 更新积分活动 | `security` | id(path):string*; [application/json] code:string name:string description:string type:string rewardPoints:number targetCount:number dailyLimit:number totalLimit:number validDays:number icon:string link:string isActive:boolean …:共12个字段 | （文档未说明） |
| POST | `/points/activities/{id}/claim` | PointsController_claimTaskReward | 领取任务奖励 | `security` | id(path):string* | （文档未说明） |
| POST | `/points/add` | PointsController_addPoints | 增加积分 | `security` | [application/json] amount:number* source:string* description:string relatedType:string relatedId:number validDays:number | （文档未说明） |
| GET | `/points/balance` | PointsController_getBalance | 获取积分余额 | `security` | （无） | （文档未说明） |
| POST | `/points/spend` | PointsController_spendPoints | 消费积分 | `security` | [application/json] amount:number* source:string* description:string relatedType:string relatedId:number | （文档未说明） |
| GET | `/points/statistics` | PointsController_getStatistics | 获取积分系统统计数据（管理员） | `security` | （无） | （文档未说明） |
| GET | `/points/tasks/my` | PointsController_getMyTasks | 获取我的任务记录 | `security` | （无） | （文档未说明） |
| GET | `/points/tasks/my/{activityId}` | PointsController_getMyTaskProgress | 获取指定活动的任务进度 | `security` | activityId(path):string* | （文档未说明） |
| GET | `/points/transactions` | PointsController_getTransactions | 获取积分交易记录 | `security` | page(query):number; limit(query):number; type(query):string(EARN\|SPEND\|ADMIN_ADJUST\|EXPIRE\|REFUND); source(query):string | （文档未说明） |
| GET | `/points/transactions/all` | PointsController_getAllTransactions | 获取所有用户的积分交易记录（管理员） | `security` | page(query):number; limit(query):number; type(query):string(EARN\|SPEND\|ADMIN_ADJUST\|EXPIRE\|REFUND); source(query):string | （文档未说明） |
| GET | `/points/users/{userId}/balance` | PointsController_getUserBalance | 获取指定用户的积分余额（管理员） | `security` | userId(path):string* | （文档未说明） |
| GET | `/points/users/{userId}/transactions` | PointsController_getUserTransactions | 获取指定用户的积分交易记录（管理员） | `security` | userId(path):string*; page(query):number; limit(query):number; type(query):string(EARN\|SPEND\|ADMIN_ADJUST\|EXPIRE\|REFUND); source(query):string | （文档未说明） |

### 3.6 系统配置（13 个操作）

| 方法 | 路径 | operationId | 摘要 | 鉴权 | 请求参数/请求体 | 响应关键字段 |
|---|---|---|---|---|---|---|
| GET | `/config` | ConfigController_findAll | 获取所有配置 | `security` | （无） | （文档未说明） |
| PATCH | `/config` | ConfigController_updateAll | 更新所有配置 | `security` | （无） | （文档未说明） |
| POST | `/config` | ConfigController_create | 创建配置 | `security` | （无） | （文档未说明） |
| GET | `/config/advertisement` | ConfigController_getAdvertisementConfig | 获取广告配置 | — | （无） | （文档未说明） |
| GET | `/config/group/{group}` | ConfigController_findByGroup | 根据分组获取配置 | `security` | group(path):string* | （文档未说明） |
| PATCH | `/config/group/{group}` | ConfigController_updateGroup | 更新分组配置 | `security` | group(path):string* | （文档未说明） |
| GET | `/config/public` | ConfigController_getPublicConfigs | 获取所有公共配置 | — | （无） | （文档未说明） |
| DELETE | `/config/sensitive-words` | ConfigController_removeSensitiveWord | 删除敏感词 | `security` | （无） | （文档未说明） |
| GET | `/config/sensitive-words` | ConfigController_findAllSensitiveWords | 获取敏感词列表 | `security` | （无） | （文档未说明） |
| POST | `/config/sensitive-words` | ConfigController_createSensitiveWord | 添加敏感词 | `security` | （无） | （文档未说明） |
| DELETE | `/config/sensitive-words/batch` | ConfigController_batchRemoveSensitiveWords | 批量删除敏感词 | `security` | （无） | （文档未说明） |
| POST | `/config/sensitive-words/batch` | ConfigController_batchCreateSensitiveWords | 批量添加敏感词 | `security` | （无） | （文档未说明） |
| POST | `/config/sensitive-words/clear-cache` | ConfigController_clearSensitiveWordsCache | 清除敏感词缓存 | `security` | （无） | （文档未说明） |

### 3.7 表情包管理（13 个操作）

| 方法 | 路径 | operationId | 摘要 | 鉴权 | 请求参数/请求体 | 响应关键字段 |
|---|---|---|---|---|---|---|
| GET | `/emoji` | EmojiController_findAll | 获取表情列表 | — | page(query):number; limit(query):number; grouped(query):boolean; type(query):string(system\|user); category(query):string; keyword(query):string; isPublic(query):boolean; status(query):string(active\|inactive\|deleted); userId(query):string; onlyFavorites(query):boolean; sortBy(query):string; sortOrder(query):string(ASC\|DESC) | （文档未说明） |
| POST | `/emoji` | EmojiController_create | 创建表情 | `security` | [application/json] name:string* url:string* code:string type:string* category:string tags:string isPublic:boolean* width:number height:number fileSize:number mimeType:string | （文档未说明） |
| GET | `/emoji/categories/list` | EmojiController_getCategories | 获取表情分类列表 | — | （无） | （文档未说明） |
| GET | `/emoji/favorites/list` | EmojiController_getFavorites | 获取我的收藏 | `security` | page(query):number; limit(query):number | （文档未说明） |
| GET | `/emoji/popular/list` | EmojiController_getPopular | 获取热门表情 | — | limit(query):number | （文档未说明） |
| GET | `/emoji/recent/list` | EmojiController_getRecent | 获取最近添加的表情 | `security` | limit(query):number | （文档未说明） |
| POST | `/emoji/upload` | EmojiController_upload | 上传表情图片 | `security` | [multipart/form-data] name:string* code:string category:string tags:string isPublic:boolean* | （文档未说明） |
| DELETE | `/emoji/{id}` | EmojiController_remove | 删除表情 | `security` | id(path):string* | （文档未说明） |
| GET | `/emoji/{id}` | EmojiController_findOne | 获取单个表情 | — | id(path):string* | （文档未说明） |
| PATCH | `/emoji/{id}` | EmojiController_update | 更新表情 | `security` | id(path):string*; [application/json] name:string url:string code:string type:string category:string tags:string isPublic:boolean width:number height:number fileSize:number mimeType:string status:string | （文档未说明） |
| DELETE | `/emoji/{id}/favorite` | EmojiController_removeFromFavorites | 取消收藏 | `security` | id(path):string* | （文档未说明） |
| POST | `/emoji/{id}/favorite` | EmojiController_addToFavorites | 添加到收藏 | `security` | id(path):string* | （文档未说明） |
| POST | `/emoji/{id}/use` | EmojiController_incrementUseCount | 增加使用次数 | `security` | id(path):string* | （文档未说明） |

### 3.8 上传管理（10 个操作）

| 方法 | 路径 | operationId | 摘要 | 鉴权 | 请求参数/请求体 | 响应关键字段 |
|---|---|---|---|---|---|---|
| GET | `/upload` | UploadController_findAll | 获取所有上传文件 | `security` | page(query):number; limit(query):number; sortBy(query):string*; sortOrder(query):string* | （文档未说明） |
| POST | `/upload/avatar` | UploadController_uploadAvatar | 上传头像（不添加水印） | `security` | [application/json] files:array<string> metadata:string | （文档未说明） |
| GET | `/upload/config` | UploadController_getUploadConfig | 获取上传配置 | `security` | （无） | （文档未说明） |
| POST | `/upload/confirm` | UploadController_confirmUpload | 确认 S3 直传完成 | `security` | [application/json] key:string* url:string* originalName:string* mimeType:string* size:number* hash:string scene:string | （文档未说明） |
| POST | `/upload/file` | UploadController_uploadFile | 上传文件 | `security` | [application/json] files:array<string> metadata:string scene:string | （文档未说明） |
| GET | `/upload/info/{id}` | UploadController_getFileInfo | 获取文件信息 | `security` | id(path):string* | （文档未说明） |
| POST | `/upload/migration/metadata` | UploadController_fillMissingMetadata | 补充图片元数据（管理员） | `security` | limit(query):string* | （文档未说明） |
| POST | `/upload/migration/process` | UploadController_processUncompressedImages | 批量处理未压缩图片（管理员） | `security` | limit(query):string*; dryRun(query):string*; [application/json] limit:number dryRun:boolean | （文档未说明） |
| GET | `/upload/migration/stats` | UploadController_getMigrationStats | 获取压缩处理统计（管理员） | `security` | （无） | （文档未说明） |
| POST | `/upload/presign` | UploadController_generatePresignedUrl | 获取 S3 预签名上传 URL（直传） | `security` | [application/json] filename:string* contentType:string* size:number* | （文档未说明） |

### 3.9 合集管理（10 个操作）

| 方法 | 路径 | operationId | 摘要 | 鉴权 | 请求参数/请求体 | 响应关键字段 |
|---|---|---|---|---|---|---|
| GET | `/collection` | CollectionController_findAll | 获取合集列表 | — | page(query):number; limit(query):number; userId(query):number; keyword(query):string; sortBy(query):string; sortOrder(query):string(ASC\|DESC) | （文档未说明） |
| POST | `/collection` | CollectionController_create | 创建合集 | `security` | [application/json] name:string* description:string avatar:string cover:string isPublic:boolean sort:number | （文档未说明） |
| GET | `/collection/article/{articleId}/info` | CollectionController_getArticleCollectionInfo | 获取文章所在的合集信息 | `security` | articleId(path):number* | （文档未说明） |
| GET | `/collection/check/{articleId}` | CollectionController_checkArticleInCollections | 检查文章是否在合集中 | `security` | articleId(path):number* | （文档未说明） |
| DELETE | `/collection/{id}` | CollectionController_remove | 删除合集 | `security` | id(path):number* | （文档未说明） |
| GET | `/collection/{id}` | CollectionController_findOne | 获取合集详情 | — | id(path):number* | （文档未说明） |
| PATCH | `/collection/{id}` | CollectionController_update | 更新合集 | `security` | id(path):number*; [application/json] name:string description:string avatar:string cover:string isPublic:boolean sort:number | （文档未说明） |
| DELETE | `/collection/{id}/article/{articleId}` | CollectionController_removeFromCollection | 从合集移除文章 | `security` | id(path):number*; articleId(path):number* | （文档未说明） |
| POST | `/collection/{id}/article/{articleId}` | CollectionController_addToCollection | 添加文章到合集 | `security` | id(path):number*; articleId(path):number* | （文档未说明） |
| GET | `/collection/{id}/items` | CollectionController_getCollectionItems | 获取合集中的文章列表 | — | id(path):number*; page(query):number; limit(query):number | （文档未说明） |

### 3.10 角色管理（10 个操作）

| 方法 | 路径 | operationId | 摘要 | 鉴权 | 请求参数/请求体 | 响应关键字段 |
|---|---|---|---|---|---|---|
| GET | `/role` | RoleController_findAll | 获取所有角色 | `security` | （无） | （文档未说明） |
| POST | `/role` | RoleController_create | 创建角色 | `security` | [application/json] name:string* displayName:string description:string* permissionIds:array<string> isActive:boolean isSystem:boolean | （文档未说明） |
| GET | `/role/active` | RoleController_getActiveRoles | 获取活跃角色列表 | `security` | （无） | （文档未说明） |
| GET | `/role/list` | RoleController_findWithPagination | 分页获取角色列表 | `security` | page(query):number; limit(query):number; name(query):string; isActive(query):boolean | （文档未说明） |
| DELETE | `/role/{id}` | RoleController_remove | 删除角色 | `security` | id(path):string* | （文档未说明） |
| GET | `/role/{id}` | RoleController_findOne | 根据ID获取角色 | `security` | id(path):string* | （文档未说明） |
| PATCH | `/role/{id}` | RoleController_update | 更新角色 | `security` | id(path):string*; [application/json] name:string displayName:string description:string permissionIds:array<number> isActive:boolean isSystem:boolean | （文档未说明） |
| POST | `/role/{id}/copy` | RoleController_copyRole | 复制角色 | `security` | id(path):string* | （文档未说明） |
| POST | `/role/{id}/permissions` | RoleController_assignPermissions | 为角色分配权限 | `security` | id(path):string*; [application/json] permissionIds:array<number>* | （文档未说明） |
| PATCH | `/role/{id}/status` | RoleController_toggleStatus | 启用/禁用角色 | `security` | id(path):string* | （文档未说明） |

### 3.11 订单管理（10 个操作）

| 方法 | 路径 | operationId | 摘要 | 鉴权 | 请求参数/请求体 | 响应关键字段 |
|---|---|---|---|---|---|---|
| GET | `/order` | OrderController_getAllOrders | 获取所有订单列表（管理员权限） | `security` | page(query):number; limit(query):number; status(query):string(PENDING\|PAID\|CANCELLED\|REFUNDED); type(query):string(MEMBERSHIP\|PRODUCT\|SERVICE\|ARTICLE); userId(query):number; keyword(query):string | （文档未说明） |
| POST | `/order/article` | OrderController_createArticleOrder | 创建文章订单 | `security` | [application/json] articleId:number* remark:string | （文档未说明） |
| POST | `/order/membership` | OrderController_createMembershipOrder | 创建会员充值订单 | `security` | [application/json] plan:string duration:number remark:string | （文档未说明） |
| GET | `/order/no/{orderNo}` | OrderController_findByOrderNo | 根据订单号获取订单 | `security` | orderNo(path):string* | （文档未说明） |
| GET | `/order/pending` | OrderController_getPendingOrders | 获取待支付订单 | `security` | （无） | （文档未说明） |
| GET | `/order/user` | OrderController_getUserOrders | 获取用户订单列表 | `security` | page(query):number; limit(query):number; status(query):string(PENDING\|PAID\|CANCELLED\|REFUNDED); type(query):string(MEMBERSHIP\|PRODUCT\|SERVICE\|ARTICLE); keyword(query):string | （文档未说明） |
| GET | `/order/wallet/balance` | OrderController_getWalletBalance | 获取钱包余额 | `security` | （无） | （文档未说明） |
| GET | `/order/{id}` | OrderController_findOne | 获取订单详情 | `security` | id(path):string* | （文档未说明） |
| PUT | `/order/{id}/cancel` | OrderController_cancelOrder | 取消订单 | `security` | id(path):string* | （文档未说明） |
| POST | `/order/{id}/refund` | OrderController_requestRefund | 申请退款 | `security` | id(path):string* | （文档未说明） |

### 3.12 评论管理（10 个操作）

| 方法 | 路径 | operationId | 摘要 | 鉴权 | 请求参数/请求体 | 响应关键字段 |
|---|---|---|---|---|---|---|
| GET | `/comment` | CommentController_findAllComments | 获取全部评论 | `security` | page(query):number; limit(query):number; articleId(query):string*; userId(query):string*; keyword(query):string* | （文档未说明） |
| POST | `/comment` | CommentController_create | 创建评论 | `security` | [application/json] content:string* articleId:number* parentId:number images:? | （文档未说明） |
| GET | `/comment/article/{id}` | CommentController_findAll | 获取文章评论列表 | `security` | id(path):string*; page(query):number; limit(query):number; sortBy(query):string(latest\|oldest\|hot); onlyAuthor(query):boolean | （文档未说明） |
| GET | `/comment/article/{id}/count` | CommentController_getCommentCount | 获取文章评论数量 | `security` | id(path):string* | （文档未说明） |
| GET | `/comment/user/{userId}` | CommentController_getUserComments | 获取用户评论列表 | `security` | userId(path):string*; page(query):number; limit(query):number | （文档未说明） |
| DELETE | `/comment/{id}` | CommentController_remove | 删除评论 | `security` | id(path):string* | （文档未说明） |
| GET | `/comment/{id}` | CommentController_findOne | 获取评论详情 | `security` | id(path):string*; page(query):number; limit(query):number; sortBy(query):string(latest\|oldest\|hot); onlyAuthor(query):boolean | （文档未说明） |
| PATCH | `/comment/{id}` | CommentController_update | 更新评论 | `security` | id(path):string*; [application/json] content:string images:array<string> isPinned:boolean | （文档未说明） |
| POST | `/comment/{id}/like` | CommentController_like | 点赞评论 | `security` | id(path):string* | （文档未说明） |
| PATCH | `/comment/{id}/pin` | CommentController_setPin | 设置评论置顶状态 | `security` | id(path):string*; [application/json] isPinned:boolean* | （文档未说明） |

### 3.13 标签管理（9 个操作）

| 方法 | 路径 | operationId | 摘要 | 鉴权 | 请求参数/请求体 | 响应关键字段 |
|---|---|---|---|---|---|---|
| GET | `/tag` | TagController_findAll | 获取所有标签 | `security` | page(query):number; limit(query):number; name(query):string*; sortBy(query):string*; sortOrder(query):string* | （文档未说明） |
| POST | `/tag` | TagController_create | 创建标签 | `security` | [application/json] name:string* description:string avatar:string* background:string* cover:string* sort:number* | （文档未说明） |
| GET | `/tag/followed/list` | TagController_getFollowedTags | 获取用户关注的标签列表 | `security` | page(query):number; limit(query):number; name(query):string*; userId(query):number | （文档未说明） |
| GET | `/tag/recommended` | TagController_getRecommendedTags | 获取推荐标签 | `security` | page(query):number; limit(query):number | （文档未说明） |
| DELETE | `/tag/{id}` | TagController_remove | 删除标签 | `security` | id(path):string* | （文档未说明） |
| GET | `/tag/{id}` | TagController_findOne | 获取标签详情 | `security` | id(path):string* | （文档未说明） |
| PATCH | `/tag/{id}` | TagController_update | 更新标签 | `security` | id(path):string*; [application/json] name:string description:string avatar:string background:string cover:string sort:number | （文档未说明） |
| DELETE | `/tag/{id}/follow` | TagController_unfollow | 取消关注标签 | `security` | id(path):string* | （文档未说明） |
| POST | `/tag/{id}/follow` | TagController_follow | 关注标签 | `security` | id(path):string* | （文档未说明） |

### 3.14 成就管理（8 个操作）

| 方法 | 路径 | operationId | 摘要 | 鉴权 | 请求参数/请求体 | 响应关键字段 |
|---|---|---|---|---|---|---|
| GET | `/achievement` | AchievementController_findAll | 获取成就列表 | `security` | keyword(query):string; sortBy(query):string*; sortOrder(query):string* | （文档未说明） |
| POST | `/achievement` | AchievementController_create | 创建成就（管理员） | `security` | [application/json] code:string* name:string* description:string* icon:string type:string* rarity:string* condition:object* rewardPoints:number* rewardExp:number* rewardDecorationId:number hidden:boolean* sort:number* …:共12个字段 | （文档未说明） |
| POST | `/achievement/claim-all` | AchievementController_claimAllRewards | 一键领取所有成就奖励 | `security` | （无） | （文档未说明） |
| GET | `/achievement/stats` | AchievementController_getUserStats | 获取用户成就统计 | `security` | （无） | （文档未说明） |
| DELETE | `/achievement/{id}` | AchievementController_remove | 删除成就（管理员） | `security` | id(path):string* | （文档未说明） |
| GET | `/achievement/{id}` | AchievementController_findOne | 获取成就详情 | `security` | id(path):string* | （文档未说明） |
| PATCH | `/achievement/{id}` | AchievementController_update | 更新成就（管理员） | `security` | id(path):string*; [application/json] code:string name:string description:string icon:string type:string rarity:string condition:object rewardPoints:number rewardExp:number rewardDecorationId:number hidden:boolean sort:number …:共12个字段 | （文档未说明） |
| POST | `/achievement/{id}/claim` | AchievementController_claimReward | 领取成就奖励 | `security` | id(path):string* | （文档未说明） |

### 3.15 支付管理（7 个操作）

| 方法 | 路径 | operationId | 摘要 | 鉴权 | 请求参数/请求体 | 响应关键字段 |
|---|---|---|---|---|---|---|
| POST | `/payment/create` | PaymentController_createPayment | 创建支付 | `security` | [application/json] orderId:number* paymentMethod:string* returnUrl:string type:string | （文档未说明） |
| POST | `/payment/notify/alipay` | PaymentController_alipayNotify | 支付宝支付回调 | — | [application/json] trade_no:string* out_trade_no:string* trade_status:string* total_amount:string* buyer_id:string* sign:string* sign_type:string* | （文档未说明） |
| GET | `/payment/notify/epay` | PaymentController_epayNotify | 易支付支付回调 | — | pid(query):string*; trade_no(query):string*; out_trade_no(query):string*; trade_status(query):string*; money(query):string*; type(query):string*; name(query):string*; sign(query):string*; sign_type(query):string* | （文档未说明） |
| POST | `/payment/notify/wechat` | PaymentController_wechatNotify | 微信支付回调 | — | [application/json] transaction_id:string* out_trade_no:string* trade_state:string* amount:string* openid:string* sign:string* | （文档未说明） |
| GET | `/payment/order/{orderId}` | PaymentController_findPaymentByOrderId | 查询订单支付记录 | `security` | orderId(path):number* | （文档未说明） |
| GET | `/payment/record/{id}` | PaymentController_findPaymentRecord | 查询支付记录 | `security` | id(path):number* | （文档未说明） |
| GET | `/payment/user` | PaymentController_findUserPayments | 查询用户支付记录 | `security` | page(query):number*; limit(query):number* | （文档未说明） |

### 3.16 举报管理（6 个操作）

| 方法 | 路径 | operationId | 摘要 | 鉴权 | 请求参数/请求体 | 响应关键字段 |
|---|---|---|---|---|---|---|
| GET | `/report` | ReportController_findAll | 获取举报列表 | `security` | page(query):number; limit(query):number; type(query):string(USER\|ARTICLE\|COMMENT); status(query):string(PENDING\|PROCESSING\|RESOLVED\|REJECTED); category(query):string(SPAM\|ABUSE\|INAPPROPRIATE\|COPYRIGHT\|OTHER); reporterId(query):number; keyword(query):string; sortBy(query):string; sortOrder(query):string(ASC\|DESC) | （文档未说明） |
| POST | `/report` | ReportController_create | 创建举报 | `security` | [application/json] type:string* reason:string* category:string* description:string reportedUserId:number reportedArticleId:number reportedCommentId:number | （文档未说明） |
| GET | `/report/statistics` | ReportController_getStatistics | 获取举报统计 | `security` | （无） | （文档未说明） |
| DELETE | `/report/{id}` | ReportController_remove | 删除举报记录 | `security` | id(path):string* | （文档未说明） |
| GET | `/report/{id}` | ReportController_findOne | 获取举报详情 | `security` | id(path):string* | （文档未说明） |
| PATCH | `/report/{id}` | ReportController_update | 更新举报状态 | `security` | id(path):string*; [application/json] status:string action:string result:string | （文档未说明） |

### 3.17 内容审核（6 个操作）

| 方法 | 路径 | operationId | 摘要 | 鉴权 | 请求参数/请求体 | 响应关键字段 |
|---|---|---|---|---|---|---|
| POST | `/content-audit/articles/{id}/review` | ContentAuditController_reviewArticle | 审核文章 | `security` | id(path):number*; [application/json] result:string* reason:string | （文档未说明） |
| GET | `/content-audit/config` | ContentAuditController_getConfig | 获取审核配置 | `security` | （无） | （文档未说明） |
| POST | `/content-audit/config/reload` | ContentAuditController_reloadConfig | 重新加载审核配置 | `security` | （无） | （文档未说明） |
| POST | `/content-audit/image` | ContentAuditController_auditImage | 测试图片审核 | `security` | （无） | （文档未说明） |
| GET | `/content-audit/pending-articles` | ContentAuditController_getPendingArticles | 获取待审核文章列表 | `security` | page(query):number; limit(query):number | （文档未说明） |
| POST | `/content-audit/text` | ContentAuditController_auditText | 测试文本审核 | `security` | （无） | （文档未说明） |

### 3.18 轮播管理（6 个操作）

| 方法 | 路径 | operationId | 摘要 | 鉴权 | 请求参数/请求体 | 响应关键字段 |
|---|---|---|---|---|---|---|
| GET | `/banners` | BannerController_findAll | 获取所有轮播 | `security` | page(query):number; limit(query):number; status(query):string; sortBy(query):string*; sortOrder(query):string* | （文档未说明） |
| POST | `/banners` | BannerController_create | 创建轮播 | `security` | [application/json] title:string* description:string imageUrl:string* linkUrl:string sortOrder:number* status:string | （文档未说明） |
| GET | `/banners/active` | BannerController_findActive | 获取活动轮播 | `security` | （无） | （文档未说明） |
| DELETE | `/banners/{id}` | BannerController_remove | 删除轮播 | `security` | id(path):number* | （文档未说明） |
| GET | `/banners/{id}` | BannerController_findOne | 获取轮播详情 | `security` | id(path):number* | （文档未说明） |
| PATCH | `/banners/{id}` | BannerController_update | 更新轮播 | `security` | id(path):number* | （文档未说明） |

### 3.19 邀请管理（6 个操作）

| 方法 | 路径 | operationId | 摘要 | 鉴权 | 请求参数/请求体 | 响应关键字段 |
|---|---|---|---|---|---|---|
| POST | `/invite` | InviteController_createInvite | 创建邀请码 | `security` | [application/json] type:string* commissionRate:number expiredAt:string remark:string | （文档未说明） |
| GET | `/invite/earnings` | InviteController_getMyInviteEarnings | 获取邀请收益记录 | `security` | page(query):number; limit(query):number | （文档未说明） |
| GET | `/invite/my` | InviteController_getMyInvites | 获取我的邀请列表 | `security` | page(query):number; limit(query):number; keyword(query):string*; status(query):string* | （文档未说明） |
| GET | `/invite/stats` | InviteController_getInviteStats | 获取邀请统计信息 | `security` | （无） | （文档未说明） |
| POST | `/invite/use` | InviteController_useInvite | 使用邀请码 | `security` | [application/json] inviteCode:string* | （文档未说明） |
| GET | `/invite/{id}` | InviteController_getInviteDetail | 获取邀请详情 | `security` | id(path):string* | （文档未说明） |

### 3.20 分类管理（5 个操作）

| 方法 | 路径 | operationId | 摘要 | 鉴权 | 请求参数/请求体 | 响应关键字段 |
|---|---|---|---|---|---|---|
| GET | `/category` | CategoryController_findAll | 获取所有分类 | `security` | page(query):number; limit(query):number; name(query):string*; status(query):string*; parentId(query):number*; sortBy(query):string*; sortOrder(query):string* | （文档未说明） |
| POST | `/category` | CategoryController_create | 创建分类 | `security` | [application/json] name:string* description:string sort:number* parentId:number link:string avatar:string background:string cover:string status:string* | （文档未说明） |
| DELETE | `/category/{id}` | CategoryController_remove | 删除分类 | `security` | id(path):string* | （文档未说明） |
| GET | `/category/{id}` | CategoryController_findOne | 获取分类详情 | `security` | id(path):string* | （文档未说明） |
| PATCH | `/category/{id}` | CategoryController_update | 更新分类 | `security` | id(path):string*; [application/json] name:string description:string sort:number parentId:number link:string avatar:string background:string cover:string status:string | （文档未说明） |

### 3.21 权限管理（5 个操作）

| 方法 | 路径 | operationId | 摘要 | 鉴权 | 请求参数/请求体 | 响应关键字段 |
|---|---|---|---|---|---|---|
| GET | `/permission` | PermissionController_findAll | 获取所有权限 | `security` | （无） | （文档未说明） |
| POST | `/permission` | PermissionController_create | 创建权限 | `security` | [application/json] name:string* description:string* | （文档未说明） |
| DELETE | `/permission/{id}` | PermissionController_remove | 删除权限 | `security` | id(path):string* | （文档未说明） |
| GET | `/permission/{id}` | PermissionController_findOne | 根据ID获取权限 | `security` | id(path):string* | （文档未说明） |
| PATCH | `/permission/{id}` | PermissionController_update | 更新权限 | `security` | id(path):string*; [application/json] name:string description:string | （文档未说明） |

### 3.22 搜索管理（3 个操作）

| 方法 | 路径 | operationId | 摘要 | 鉴权 | 请求参数/请求体 | 响应关键字段 |
|---|---|---|---|---|---|---|
| POST | `/search/clear/articles` | SearchController_clearArticles | 清空文章搜索索引 | `security` | （无） | （文档未说明） |
| GET | `/search/status` | SearchController_getStatus | 获取 Elasticsearch 状态 | `security` | （无） | （文档未说明） |
| POST | `/search/sync/articles` | SearchController_syncArticles | 同步文章到 Elasticsearch（全量同步） | `security` | （无） | （文档未说明） |

### 3.23 统计模块（2 个操作）

| 方法 | 路径 | operationId | 摘要 | 鉴权 | 请求参数/请求体 | 响应关键字段 |
|---|---|---|---|---|---|---|
| GET | `/statistics/overview` | StatisticsController_getOverview | 获取后台统计概览 | `security` | （无） | （文档未说明） |
| GET | `/statistics/trends` | StatisticsController_getTrends | 获取最近趋势统计 | `security` | days(query):number | （文档未说明） |

### 3.24 App（1 个操作）

| 方法 | 路径 | operationId | 摘要 | 鉴权 | 请求参数/请求体 | 响应关键字段 |
|---|---|---|---|---|---|---|
| GET | `/` | AppController_getHello |  | — | （无） | （文档未说明） |

## 4. 数据模型（components.schemas，被端点引用的核心 schema）

共 74 个 schema，其中被端点引用的 74 个，全部列出。

#### AddPointsDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| amount | number | ✓ | 积分数量 |
| source | string | ✓ | 积分来源 |
| description | string |  | 描述 |
| relatedType | string |  | 关联业务类型 |
| relatedId | number |  | 关联业务ID |
| validDays | number |  | 有效天数（0为永久） |

#### AlipayNotifyDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| trade_no | string | ✓ | 支付宝交易号 |
| out_trade_no | string | ✓ | 商户订单号 |
| trade_status | string | ✓ | 交易状态 |
| total_amount | string | ✓ | 交易金额 |
| buyer_id | string | ✓ | 买家支付宝用户号 |
| sign | string | ✓ | 签名 |
| sign_type | string | ✓ | 签名类型 |

#### ApplyCertificationDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| type | string | ✓ | 认证类型：OFFICIAL-官方，CREATOR-创作者 枚举:OFFICIAL\|CREATOR |
| realName | string |  | 真实姓名/主体名称 |
| organizationName | string |  | 机构名称（官方认证必填） |
| description | string |  | 认证说明 |
| slogan | string |  | 认证标语（公开展示，长度不超过80字符，勿填私人信息） |
| materials | array<string> |  | 证明材料（文件URL数组） |

#### ArticleDislikeDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| reason | string |  | 不感兴趣原因 |

#### ArticleLikeDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| reactionType | string | ✓ | 表情类型 枚举:like\|love\|haha\|wow\|sad\|angry\|dislike |

#### AssignPermissionsDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| permissionIds | array<number> | ✓ | 权限ID列表 |

#### AuditCertificationDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| status | string | ✓ | 审核结果：APPROVED-通过，REJECTED-拒绝 枚举:APPROVED\|REJECTED |
| reason | string |  | 审核意见/拒绝原因（拒绝时建议填写） |

#### Banner

（文档未说明）

#### BatchMessageDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| messageIds | array<string> |  | 消息ID列表 |
| action | string |  | 操作类型 枚举:read\|delete |

#### BatchReadPrivateMessagesDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| messageIds | array<string> |  |  |

#### CalculateCommissionDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| amount | number |  | 金额 |
| type | string |  | 抽成类型 枚举:article\|membership\|product\|service |

#### ChangePasswordDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| oldPassword | string |  | 旧密码 |
| newPassword | string |  | 新密码 |

#### CreateAchievementDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| code | string |  | 成就代码（唯一标识） |
| name | string |  | 成就名称 |
| description | string |  | 成就描述 |
| icon | string |  | 成就图标URL |
| type | string |  | 成就类型 枚举:ARTICLE\|COMMENT\|SOCIAL\|LEVEL\|SPECIAL |
| rarity | string |  | 稀有度 枚举:COMMON\|RARE\|EPIC\|LEGENDARY |
| condition | object |  | 完成条件 |
| rewardPoints | number |  | 奖励积分 |
| rewardExp | number |  | 奖励经验 |
| rewardDecorationId | number |  | 奖励装饰品ID |
| hidden | boolean |  | 是否隐藏 |
| sort | number |  | 排序 |
| enabled | boolean |  | 是否启用 |

#### CreateActivityDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| name | string |  | 活动名称 |
| description | string |  | 活动描述 |
| type | string |  | 活动类型 枚举:LIKE\|COMMENT\|SHARE\|RECHARGE\|SIGN_IN\|CUSTOM |
| decorationId | number |  | 奖励装饰品ID |
| articleId | number |  | 关联文章ID（活动说明） |
| requiredLikes | number |  | 所需点赞数 |
| requiredComments | number |  | 所需评论数 |
| requiredShares | number |  | 所需分享数 |
| requiredRecharge | number |  | 所需充值金额 |
| requiredSignInDays | number |  | 所需签到天数 |
| isPermanent | boolean |  | 奖励是否永久 |
| validDays | number |  | 奖励有效天数 |
| startTime | string |  | 开始时间 |
| endTime | string |  | 结束时间 |

#### CreateArticleDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| title | string |  | 文章标题 |
| content | string |  | 文章内容 |
| summary | string |  | 文章摘要 |
| images | ? |  | 文章图片，传单个URL字符串或URL数组 |
| videoUrl | string |  | 视频链接，当type为video时必填 |
| cover | string |  | 封面图片 |
| sort | number |  | 排序 |
| categoryId | number |  | 分类ID |
| tagNames | array<string> |  | 标签名称数组（不存在的标签会自动创建） |
| tagIds | array<string> |  | 标签ID数组（与tagNames二选一） |
| status | string |  | 文章状态 枚举:DRAFT\|PUBLISHED |
| requireLogin | boolean |  | 是否需要登录后才能查看 |
| requireFollow | boolean |  | 是否仅关注后可查看 |
| requirePayment | boolean |  | 是否需要支付后才能查看 |
| requireMembership | boolean |  | 是否需要会员才能查看 |
| listRequireLogin | boolean |  | 仅登录后才在列表显示 |
| viewPrice | number |  | 查看所需支付金额 |
| type | string |  | 文章类型 枚举:image\|mixed\|video |
| activityId | number |  | 关联活动ID |
| downloads | array<DownloadDto> |  | 下载资源列表 |
| isFeatured | boolean |  | 是否设为精华，仅管理员可设置 |
| isPinnedOnProfile | boolean |  | 是否在个人主页置顶，作者或管理员可设置 |

#### CreateArticleOrderDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| articleId | number |  | 文章ID |
| remark | string |  | 备注 |

#### CreateBannerDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| title | string |  | 轮播标题 |
| description | string |  | 轮播描述 |
| imageUrl | string |  | 轮播图片URL |
| linkUrl | string |  | 跳转链接URL |
| sortOrder | number |  | 排序顺序（数字越大越靠前） |
| status | string |  | 轮播状态 枚举:active\|inactive |

#### CreateCategoryDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| name | string |  | 分类名称 |
| description | string |  | 分类描述 |
| sort | number |  | 排序 |
| parentId | number |  | 父分类ID |
| link | string |  | 自定义链接 |
| avatar | string |  | 分类头像 |
| background | string |  | 分类背景 |
| cover | string |  | 分类封面 |
| status | string |  | 分类状态 |

#### CreateCollectionDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| name | string |  | 合集名称 |
| description | string |  | 合集描述 |
| avatar | string |  | 头像 |
| cover | string |  | 封面图片 |
| isPublic | boolean |  | 是否公开 |
| sort | number |  | 排序 |

#### CreateCommentDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| content | string |  | 评论内容 |
| articleId | number |  | 文章ID |
| parentId | number |  | 父评论ID |
| images | ? |  | 评论图片，传单个URL字符串或URL数组（最多9张） |

#### CreateConfigDto

（文档未说明）

#### CreateDecorationDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| name | string |  | 装饰品名称 |
| type | string |  | 装饰品类型 枚举:AVATAR_FRAME\|COMMENT_BUBBLE\|ACHIEVEMENT_BADGE |
| description | string |  | 装饰品描述 |
| imageUrl | string |  | 装饰品图片URL |
| previewUrl | string |  | 预览图URL |
| rarity | string |  | 稀有度 枚举:COMMON\|RARE\|EPIC\|LEGENDARY |
| bubbleColor | string |  | 气泡颜色 |
| obtainMethod | string |  | 获取方式 枚举:PURCHASE\|ACTIVITY\|GIFT\|ACHIEVEMENT\|DEFAULT |
| isPurchasable | boolean |  | 是否可购买 |
| price | number |  | 购买价格 |
| isPermanent | boolean |  | 是否永久 |
| validDays | number |  | 有效天数（0或999表示永久） |
| sort | number |  | 排序 |
| requiredLikes | number |  | 所需点赞数 |
| requiredComments | number |  | 所需评论数 |

#### CreateEmojiDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| name | string |  | 表情名称 |
| url | string |  | 表情图片URL |
| code | string |  | 表情代码 |
| type | string |  | 表情类型 枚举:system\|user |
| category | string |  | 分类 |
| tags | string |  | 标签（逗号分隔） |
| isPublic | boolean |  | 是否公开 |
| width | number |  | 宽度 |
| height | number |  | 高度 |
| fileSize | number |  | 文件大小 |
| mimeType | string |  | 文件类型 |

#### CreateInviteDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| type | string |  | 邀请类型 枚举:GENERAL\|VIP |
| commissionRate | number |  | 邀请分成比例（0-1之间） |
| expiredAt | string |  | 过期时间 |
| remark | string |  | 备注 |

#### CreateMembershipOrderDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| plan | string |  | 套餐：1m/3m/6m/12m/lifetime（可选） |
| duration | number |  | 充值时长（月）（当未选择套餐时必填） |
| remark | string |  | 备注 |

#### CreateMessageDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| senderId | object |  | 发送者ID |
| receiverId | number |  | 接收者ID（单发） |
| receiverIds | array<string> |  | 接收者ID列表（批量） |
| content | string |  | 消息内容 |
| title | string |  | 消息标题 |
| type | string |  | 消息类型 枚举:private\|system\|notification |
| isBroadcast | boolean |  | 是否为广播消息 |
| metadata | object |  | 消息元数据 |

#### CreatePaymentDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| orderId | number |  | 订单ID |
| paymentMethod | string |  | 支付方式 枚举:ALIPAY\|WECHAT\|BALANCE\|EPAY |
| returnUrl | string |  | 支付完成后的跳转地址 |
| type | string |  | 支付类型（EPAY支付方式时必传） 枚举:wxpay\|alipay\|usdt |

#### CreatePermissionDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| name | string |  | 权限名称 |
| description | string |  | 权限描述 |

#### CreatePointsActivityDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| code | string |  | 活动代码 |
| name | string |  | 活动名称 |
| description | string |  | 活动描述 |
| type | string |  | 活动类型 枚举:INSTANT\|DAILY\|WEEKLY\|MONTHLY\|ONCE |
| rewardPoints | number |  | 奖励积分 |
| targetCount | number |  | 目标数量 |
| dailyLimit | number |  | 每日限制次数（0为不限制） |
| totalLimit | number |  | 总限制次数（0为不限制） |
| validDays | number |  | 积分有效期（天数，0为永久） |
| icon | string |  | 活动图标 |
| link | string |  | 跳转链接 |
| isActive | boolean |  | 是否启用 |
| sort | number |  | 排序 |

#### CreateReportDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| type | string |  | 举报类型 枚举:USER\|ARTICLE\|COMMENT |
| reason | string |  | 举报原因 |
| category | string |  | 举报分类 枚举:SPAM\|ABUSE\|INAPPROPRIATE\|COPYRIGHT\|OTHER |
| description | string |  | 详细描述 |
| reportedUserId | number |  | 被举报用户ID |
| reportedArticleId | number |  | 被举报文章ID |
| reportedCommentId | number |  | 被举报评论ID |

#### CreateRoleDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| name | string |  | 角色名称 |
| displayName | string |  | 角色显示名称 |
| description | string |  | 角色描述 |
| permissionIds | array<string> |  | 权限ID列表 |
| isActive | boolean |  | 角色状态 |
| isSystem | boolean |  | 是否为系统角色 |

#### CreateTagDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| name | string |  | 标签名称 |
| description | string |  | 标签描述 |
| avatar | string |  | 标签头像 |
| background | string |  | 标签背景 |
| cover | string |  | 标签封面 |
| sort | number |  | 排序 |

#### CreateUserDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| username | string |  | 用户名 |
| nickname | string |  | 昵称 |
| password | string |  | 密码 |
| email | string |  | 邮箱 |
| phone | string |  | 手机号 |
| roleIds | array<number> |  | 角色ID列表（仅超级管理员可指定） |
| wallet | number |  | 钱包余额 |
| inviteCode | string |  | 邀请码 |
| verificationCode | string |  | 邮箱验证码 |

#### DownloadDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| type | string |  | 下载类型 枚举:baidu\|onedrive\|google\|quark\|aliyun\|dropbox\|direct\|lanzou\|mega\|other |
| url | string |  | 下载链接 |
| password | string |  | 提取密码 |
| extractionCode | string |  | 提取码 |
| visibleWithoutPermission | boolean |  | 是否在文章需要权限时仍可见 |

#### GiftDecorationDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| toUserId | number |  | 接收者用户ID |
| decorationId | number |  | 装饰品ID |
| message | string |  | 赠送留言 |

#### GithubOAuthCallbackDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| code | string |  | GitHub 授权回调返回的授权码 code |
| state | string |  | OAuth state 参数（CSRF 防护，由授权链接接口返回） |

#### LoginDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| account | ? |  | 用户名或邮箱 |
| password | string |  | 密码 |

#### MarkAllReadDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| type | string |  | 消息类型 枚举:private\|system\|notification |
| isBroadcast | boolean |  | 是否为广播消息 |

#### Permission

（文档未说明）

#### PurchaseDecorationDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| decorationId | number |  | 装饰品ID |

#### RecallPrivateMessageDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| reason | string |  | 撤回原因 |

#### RecordBrowseHistoryDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| progress | number |  | 浏览进度（百分比） |
| duration | number |  | 停留时长（秒） |

#### ReviewArticleDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| result | string |  | 审核结果 枚举:APPROVED\|REJECTED |
| reason | string |  | 拒绝原因 |

#### Role

（文档未说明）

#### SendMailDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| email | string |  | 邮箱 |
| type | string |  | 验证码类型 |

#### SendPrivateMessageDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| content | string |  | 文本内容 |
| messageKind | string |  | 消息类型 枚举:text\|image\|file\|card |
| payload | object |  | 结构化负载 |

#### SetArticleFeaturedDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| isFeatured | boolean |  | 是否设为精华 |

#### SetArticleProfilePinDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| isPinned | boolean |  | 是否在个人主页置顶 |

#### SetCommentPinDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| isPinned | boolean |  | 是否置顶评论 |

#### SpendPointsDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| amount | number |  | 积分数量 |
| source | string |  | 用途 |
| description | string |  | 描述 |
| relatedType | string |  | 关联业务类型 |
| relatedId | number |  | 关联业务ID |

#### UpdateAchievementDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| code | string |  | 成就代码（唯一标识） |
| name | string |  | 成就名称 |
| description | string |  | 成就描述 |
| icon | string |  | 成就图标URL |
| type | string |  | 成就类型 枚举:ARTICLE\|COMMENT\|SOCIAL\|LEVEL\|SPECIAL |
| rarity | string |  | 稀有度 枚举:COMMON\|RARE\|EPIC\|LEGENDARY |
| condition | object |  | 完成条件 |
| rewardPoints | number |  | 奖励积分 |
| rewardExp | number |  | 奖励经验 |
| rewardDecorationId | number |  | 奖励装饰品ID |
| hidden | boolean |  | 是否隐藏 |
| sort | number |  | 排序 |
| enabled | boolean |  | 是否启用 |

#### UpdateActivityDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| name | string |  | 活动名称 |
| description | string |  | 活动描述 |
| type | string |  | 活动类型 枚举:LIKE\|COMMENT\|SHARE\|RECHARGE\|SIGN_IN\|CUSTOM |
| decorationId | number |  | 奖励装饰品ID |
| articleId | number |  | 关联文章ID（活动说明） |
| requiredLikes | number |  | 所需点赞数 |
| requiredComments | number |  | 所需评论数 |
| requiredShares | number |  | 所需分享数 |
| requiredRecharge | number |  | 所需充值金额 |
| requiredSignInDays | number |  | 所需签到天数 |
| isPermanent | boolean |  | 奖励是否永久 |
| validDays | number |  | 奖励有效天数 |
| startTime | string |  | 开始时间 |
| endTime | string |  | 结束时间 |

#### UpdateArticleDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| title | string |  | 文章标题 |
| content | string |  | 文章内容 |
| summary | string |  | 文章摘要 |
| images | ? |  | 文章图片，传单个URL字符串或URL数组 |
| videoUrl | string |  | 视频链接，当type为video时必填 |
| cover | string |  | 封面图片 |
| sort | number |  | 排序 |
| categoryId | number |  | 分类ID |
| tagNames | array<string> |  | 标签名称数组（不存在的标签会自动创建） |
| tagIds | array<string> |  | 标签ID数组（与tagNames二选一） |
| status | string |  | 文章状态 枚举:DRAFT\|PUBLISHED |
| requireLogin | boolean |  | 是否需要登录后才能查看 |
| requireFollow | boolean |  | 是否仅关注后可查看 |
| requirePayment | boolean |  | 是否需要支付后才能查看 |
| requireMembership | boolean |  | 是否需要会员才能查看 |
| listRequireLogin | boolean |  | 仅登录后才在列表显示 |
| viewPrice | number |  | 查看所需支付金额 |
| type | string |  | 文章类型 枚举:image\|mixed\|video |
| activityId | number |  | 关联活动ID |
| downloads | array<DownloadDto> |  | 下载资源列表 |
| isFeatured | boolean |  | 是否设为精华，仅管理员可设置 |
| isPinnedOnProfile | boolean |  | 是否在个人主页置顶，作者或管理员可设置 |

#### UpdateBannerDto

（文档未说明）

#### UpdateCategoryDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| name | string |  | 分类名称 |
| description | string |  | 分类描述 |
| sort | number |  | 排序 |
| parentId | number |  | 父分类ID |
| link | string |  | 自定义链接 |
| avatar | string |  | 分类头像 |
| background | string |  | 分类背景 |
| cover | string |  | 分类封面 |
| status | string |  | 分类状态 |

#### UpdateCollectionDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| name | string |  | 合集名称 |
| description | string |  | 合集描述 |
| avatar | string |  | 头像 |
| cover | string |  | 封面图片 |
| isPublic | boolean |  | 是否公开 |
| sort | number |  | 排序 |

#### UpdateCommentDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| content | string |  | 评论内容 |
| images | array<string> |  | 评论图片列表（最多9张） |
| isPinned | boolean |  | 是否置顶，仅文章作者或管理员可设置 |

#### UpdateDecorationDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| name | string |  | 装饰品名称 |
| type | string |  | 装饰品类型 枚举:AVATAR_FRAME\|COMMENT_BUBBLE\|ACHIEVEMENT_BADGE |
| description | string |  | 装饰品描述 |
| imageUrl | string |  | 装饰品图片URL |
| previewUrl | string |  | 预览图URL |
| rarity | string |  | 稀有度 枚举:COMMON\|RARE\|EPIC\|LEGENDARY |
| bubbleColor | string |  | 气泡颜色 |
| obtainMethod | string |  | 获取方式 枚举:PURCHASE\|ACTIVITY\|GIFT\|ACHIEVEMENT\|DEFAULT |
| isPurchasable | boolean |  | 是否可购买 |
| price | number |  | 购买价格 |
| isPermanent | boolean |  | 是否永久 |
| validDays | number |  | 有效天数（0或999表示永久） |
| sort | number |  | 排序 |
| requiredLikes | number |  | 所需点赞数 |
| requiredComments | number |  | 所需评论数 |

#### UpdateEmojiDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| name | string |  | 表情名称 |
| url | string |  | 表情图片URL |
| code | string |  | 表情代码 |
| type | string |  | 表情类型 枚举:system\|user |
| category | string |  | 分类 |
| tags | string |  | 标签（逗号分隔） |
| isPublic | boolean |  | 是否公开 |
| width | number |  | 宽度 |
| height | number |  | 高度 |
| fileSize | number |  | 文件大小 |
| mimeType | string |  | 文件类型 |
| status | string |  | 状态 枚举:active\|inactive\|deleted |

#### UpdateMessageDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| content | string |  | 消息内容 |
| title | string |  | 消息标题 |
| isRead | boolean |  | 是否已读 |
| type | string |  | 消息类型 枚举:private\|system\|notification |
| isBroadcast | boolean |  | 是否为广播消息 |
| metadata | object |  | 消息元数据 |

#### UpdatePermissionDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| name | string |  | 权限名称 |
| description | string |  | 权限描述 |

#### UpdatePointsActivityDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| code | string |  | 活动代码 |
| name | string |  | 活动名称 |
| description | string |  | 活动描述 |
| type | string |  | 活动类型 枚举:INSTANT\|DAILY\|WEEKLY\|MONTHLY\|ONCE |
| rewardPoints | number |  | 奖励积分 |
| targetCount | number |  | 目标数量 |
| dailyLimit | number |  | 每日限制次数（0为不限制） |
| totalLimit | number |  | 总限制次数（0为不限制） |
| validDays | number |  | 积分有效期（天数，0为永久） |
| icon | string |  | 活动图标 |
| link | string |  | 跳转链接 |
| isActive | boolean |  | 是否启用 |
| sort | number |  | 排序 |

#### UpdateReportDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| status | string |  | 处理状态 枚举:PENDING\|PROCESSING\|RESOLVED\|REJECTED |
| action | string |  | 处理动作 枚举:DELETE_CONTENT\|BAN_USER\|WARNING\|NONE |
| result | string |  | 处理结果 |

#### UpdateRoleDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| name | string |  | 角色名称 |
| displayName | string |  | 角色显示名称 |
| description | string |  | 角色描述 |
| permissionIds | array<number> |  | 权限ID列表 |
| isActive | boolean |  | 角色状态 |
| isSystem | boolean |  | 是否为系统角色 |

#### UpdateTagDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| name | string |  | 标签名称 |
| description | string |  | 标签描述 |
| avatar | string |  | 标签头像 |
| background | string |  | 标签背景 |
| cover | string |  | 标签封面 |
| sort | number |  | 排序 |

#### UpdateUserConfigDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| articleCommissionRate | number |  | 文章抽成比例，0-1 之间 |
| membershipCommissionRate | number |  | 会员抽成比例，0-1 之间 |
| productCommissionRate | number |  | 商品抽成比例，0-1 之间 |
| serviceCommissionRate | number |  | 服务抽成比例，0-1 之间 |
| enableCustomCommission | boolean |  | 是否启用自定义抽成 |
| enableSystemNotification | boolean |  | 是否接收系统通知 |
| enableCommentNotification | boolean |  | 是否接收评论通知 |
| enableLikeNotification | boolean |  | 是否接收点赞通知 |
| enableFollowNotification | boolean |  | 是否接收关注通知 |
| enableMessageNotification | boolean |  | 是否接收私信通知 |
| enableOrderNotification | boolean |  | 是否接收订单通知 |
| enablePaymentNotification | boolean |  | 是否接收支付通知 |
| enableInviteNotification | boolean |  | 是否接收邀请通知 |
| enableEmailNotification | boolean |  | 是否接收邮件通知 |
| enableSmsNotification | boolean |  | 是否接收短信通知 |
| enablePushNotification | boolean |  | 是否接收推送通知 |
| hideFavorites | boolean |  | 是否隐藏收藏夹 |
| hideComments | boolean |  | 是否隐藏评论列表 |
| hideCollections | boolean |  | 是否隐藏收藏夹列表 |
| hideFollowers | boolean |  | 是否隐藏粉丝列表 |
| hideFollowings | boolean |  | 是否隐藏关注列表 |
| hideTags | boolean |  | 是否隐藏标签列表 |
| watermarkEnabled | boolean |  | 上传图片是否添加水印 |
| remark | string |  | 备注 |

#### UpdateUserContactDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| email | string |  | 新邮箱 |
| phone | string |  | 新手机号 |
| verificationCode | string |  | 邮箱验证码，仅修改邮箱时需要 |

#### UpdateUserDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| username | string |  | 用户名 |
| nickname | string |  | 昵称 |
| roleIds | array<number> |  | 角色ID列表 |
| wallet | number |  | 钱包余额 |
| inviteCode | string |  | 邀请码 |
| avatar | string |  | 头像 |
| description | string |  | 个人描述 |
| background | string |  | 个人背景 |
| address | string |  | 地址 |
| gender | string |  | 性别 枚举:male\|female\|other |
| birthDate | string |  | 生日 |
| membershipLevel | number |  | 会员等级 |
| membershipLevelName | string |  | 会员等级名称 |
| membershipStatus | string |  | 会员状态 枚举:ACTIVE\|INACTIVE |
| membershipStartDate | string |  | 会员开通时间 |
| membershipEndDate | string |  | 会员到期时间 |
| status | string |  | 用户状态 枚举:ACTIVE\|INACTIVE\|BANNED |
| banned | string |  | 封禁时间 |
| banReason | string |  | 封禁原因 |

#### UpdateUserNoticeDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| enableSystemNotification | boolean |  | 是否接收系统通知 |
| enableCommentNotification | boolean |  | 是否接收评论通知 |
| enableLikeNotification | boolean |  | 是否接收点赞通知 |
| enableFollowNotification | boolean |  | 是否接收关注通知 |
| enableMessageNotification | boolean |  | 是否接收私信通知 |
| enableOrderNotification | boolean |  | 是否接收订单通知 |
| enablePaymentNotification | boolean |  | 是否接收支付通知 |
| enableInviteNotification | boolean |  | 是否接收邀请通知 |
| enableEmailNotification | boolean |  | 是否接收邮件通知 |
| enableSmsNotification | boolean |  | 是否接收短信通知 |
| enablePushNotification | boolean |  | 是否接收推送通知 |

#### Upload

（文档未说明）

#### UploadEmojiDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| name | string |  | 表情名称 |
| code | string |  | 表情代码 |
| category | string |  | 分类 |
| tags | string |  | 标签（逗号分隔） |
| isPublic | boolean |  | 是否公开 |

#### UseInviteDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| inviteCode | string |  | 邀请码 |

#### UserCommissionConfigDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| articleCommissionRate | number |  | 文章抽成比例（0-1之间） |
| membershipCommissionRate | number |  | 会员抽成比例（0-1之间） |
| productCommissionRate | number |  | 商品抽成比例（0-1之间） |
| serviceCommissionRate | number |  | 服务抽成比例（0-1之间） |
| enableCustomCommission | boolean |  | 是否启用自定义抽成 |
| remark | string |  | 备注 |

#### WechatNotifyDto

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| transaction_id | string |  | 微信支付订单号 |
| out_trade_no | string |  | 商户订单号 |
| trade_state | string |  | 交易状态 |
| amount | string |  | 交易金额 |
| openid | string |  | 用户标识 |
| sign | string |  | 签名 |

未被端点直接引用的 schema（0 个）：无

## 5. 附：含枚举的字段汇总

| 字段 | 所在 schema/端点 | 枚举值 |
|---|---|---|
| type | /user/certification/apply | OFFICIAL \| CREATOR |
| status | /user/certification/admin/applications/{id}/audit | APPROVED \| REJECTED |
| gender | /user/{id} | male \| female \| other |
| membershipStatus | /user/{id} | ACTIVE \| INACTIVE |
| status | /user/{id} | ACTIVE \| INACTIVE \| BANNED |
| type | /user/commission/calculate | article \| membership \| product \| service |
| result | /content-audit/articles/{id}/review | APPROVED \| REJECTED |
| status | /article | DRAFT \| PUBLISHED |
| type | /article | image \| mixed \| video |
| sortBy | /article/search(param) | relevance \| latest \| views \| likes |
| status | /article/{id} | DRAFT \| PUBLISHED |
| type | /article/{id} | image \| mixed \| video |
| reactionType | /article/{id}/like | like \| love \| haha \| wow \| sad \| angry \| dislike |
| order | /article/browse/history(param) | newest \| oldest |
| status | /order(param) | PENDING \| PAID \| CANCELLED \| REFUNDED |
| type | /order(param) | MEMBERSHIP \| PRODUCT \| SERVICE \| ARTICLE |
| status | /order/user(param) | PENDING \| PAID \| CANCELLED \| REFUNDED |
| type | /order/user(param) | MEMBERSHIP \| PRODUCT \| SERVICE \| ARTICLE |
| type | /message | private \| system \| notification |
| messageKind | /message/private/{userId} | text \| image \| file \| card |
| type | /message/search(param) | private \| system \| notification |
| type | /message/{id} | private \| system \| notification |
| type | /message/read-all | private \| system \| notification |
| action | /message/batch | read \| delete |
| sortOrder | /collection(param) | ASC \| DESC |
| type | /points/transactions(param) | EARN \| SPEND \| ADMIN_ADJUST \| EXPIRE \| REFUND |
| type | /points/activities | INSTANT \| DAILY \| WEEKLY \| MONTHLY \| ONCE |
| type | /points/activities/{id} | INSTANT \| DAILY \| WEEKLY \| MONTHLY \| ONCE |
| type | /points/transactions/all(param) | EARN \| SPEND \| ADMIN_ADJUST \| EXPIRE \| REFUND |
| type | /points/users/{userId}/transactions(param) | EARN \| SPEND \| ADMIN_ADJUST \| EXPIRE \| REFUND |
| scene | /upload/file | avatar \| image \| other |
| scene | /upload/confirm | avatar \| image \| other |
| sortBy | /comment/article/{id}(param) | latest \| oldest \| hot |
| sortBy | /comment/{id}(param) | latest \| oldest \| hot |
| type | /invite | GENERAL \| VIP |
| paymentMethod | /payment/create | ALIPAY \| WECHAT \| BALANCE \| EPAY |
| type | /payment/create | wxpay \| alipay \| usdt |
| status | /banners | active \| inactive |
| type | /report | USER \| ARTICLE \| COMMENT |
| category | /report | SPAM \| ABUSE \| INAPPROPRIATE \| COPYRIGHT \| OTHER |
| type | /report(param) | USER \| ARTICLE \| COMMENT |
| status | /report(param) | PENDING \| PROCESSING \| RESOLVED \| REJECTED |
| category | /report(param) | SPAM \| ABUSE \| INAPPROPRIATE \| COPYRIGHT \| OTHER |
| sortOrder | /report(param) | ASC \| DESC |
| status | /report/{id} | PENDING \| PROCESSING \| RESOLVED \| REJECTED |
| action | /report/{id} | DELETE_CONTENT \| BAN_USER \| WARNING \| NONE |
| type | /decoration | AVATAR_FRAME \| COMMENT_BUBBLE \| ACHIEVEMENT_BADGE |
| rarity | /decoration | COMMON \| RARE \| EPIC \| LEGENDARY |
| obtainMethod | /decoration | PURCHASE \| ACTIVITY \| GIFT \| ACHIEVEMENT \| DEFAULT |
| type | /decoration/activity | LIKE \| COMMENT \| SHARE \| RECHARGE \| SIGN_IN \| CUSTOM |
| type | /decoration/activity/{id} | LIKE \| COMMENT \| SHARE \| RECHARGE \| SIGN_IN \| CUSTOM |
| type | /decoration/{id} | AVATAR_FRAME \| COMMENT_BUBBLE \| ACHIEVEMENT_BADGE |
| rarity | /decoration/{id} | COMMON \| RARE \| EPIC \| LEGENDARY |
| obtainMethod | /decoration/{id} | PURCHASE \| ACTIVITY \| GIFT \| ACHIEVEMENT \| DEFAULT |
| type | /emoji | system \| user |
| type | /emoji(param) | system \| user |
| status | /emoji(param) | active \| inactive \| deleted |
| sortOrder | /emoji(param) | ASC \| DESC |
| type | /emoji/{id} | system \| user |
| status | /emoji/{id} | active \| inactive \| deleted |
| type | /achievement | ARTICLE \| COMMENT \| SOCIAL \| LEVEL \| SPECIAL |
| rarity | /achievement | COMMON \| RARE \| EPIC \| LEGENDARY |
| type | /achievement/{id} | ARTICLE \| COMMENT \| SOCIAL \| LEVEL \| SPECIAL |
| rarity | /achievement/{id} | COMMON \| RARE \| EPIC \| LEGENDARY |
