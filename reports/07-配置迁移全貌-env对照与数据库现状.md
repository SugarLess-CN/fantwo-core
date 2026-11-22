# 07 — 配置迁移全貌：env 前后对照 + 数据库现状

- **日期**：2026-11-23
- **范围**：T-34a ~ T-34f 全部配置改动（含审核类 key 补齐）
- **本报告回答三个问题**：
  1. 迁移**前**的 env 配置是什么
  2. 迁移**后**的 env 配置是什么（哪些还管用、哪些变成兜底、哪些彻底不由 env 管）
  3. 数据库里**现在**配置了什么（56 条全表）

---

## 一、先说结论：env 一个都没删

**迁移没有删除任何环境变量。** 所有原来的 env 仍然有效，只是角色变了：

| 角色 | 含义 | 改它有没有用 |
|---|---|---|
| **唯一通道** | 只有 env 能配，DB 里没有对应的 key | 有用，但**必须重启** |
| **兜底默认值** | DB 优先、env 只在「DB 里没这一行 / 值为空」时生效 | 有用，但**改 DB 就能覆盖它**，改 env 要重启 |
| **已废弃** | 不再被任何代码读取 | 无用 |

判断依据不是猜的，是逐 key 核过读取点：`grep -rhoE 'Get(Bool|String|Int)\(' internal/` 得到后端真实消费的 DB key 清单，再对照 `internal/config/infra.go` 的 `SetDefault` 与 `.env.example`。

---

## 二、迁移**前**的 env 配置

### 2.1 业务/基础设施域（`internal/config/infra.go:102-136` + `.env.example`）

| env 键 | 默认值 | 迁移前的消费点 |
|---|---|---|
| `STORAGE_DRIVER` | `local` | 启动期选 local/s3，建对应 client |
| `STORAGE_LOCAL_DIR` | `./uploads` | 本地写入目录、URL 拼接、删除判断 |
| `S3_ENDPOINT` | 空 | minio client |
| `S3_BUCKET` | 空 | minio client + 对象读写 |
| `S3_ACCESS_KEY` | 空 | minio client 凭据 |
| `S3_SECRET_KEY` | 空 | minio client 凭据 |
| `S3_REGION` | `us-east-1` | minio client |
| `S3_USE_SSL` | `false` | minio client |
| `S3_FORCE_PATH_STYLE` | `true` | minio client |
| `CDN_BASE_URL` | 空 | 返回给前端的文件 URL 前缀 |
| `UPLOAD_MAX_IMAGE_MB` | `20` | 上传大小校验 |
| `UPLOAD_MAX_VIDEO_MB` | `500` | 上传大小校验 |
| `UPLOAD_MAX_AUDIO_MB` | `50` | 上传大小校验 |
| `UPLOAD_MAX_OTHER_MB` | `100` | 上传大小校验 |
| `UPLOAD_MAX_COUNT` | `10` | 单次文件数校验 |
| `UPLOAD_COMPRESSION_ENABLED` | `true` | **迁移前只用于 `/upload/config` 展示，处理链路根本不读** |
| `UPLOAD_WATERMARK_ENABLED` | `true` | 水印开关（**迁移前已是 DB 优先**，`watermark.go`） |
| `UPLOAD_THUMBNAIL_SPECS` | 四档 200/480/1024/1920 | 缩略图生成（`.env.example` 里没列，只在代码 `SetDefault`） |
| `SEARCH_ENABLED` | `false` | 搜索总开关 |
| `SEARCH_ADDRESSES` | 空 | ES 地址（JSON 数组或逗号分隔，与 `SEARCH_ADDRESS` 取并集） |
| `SEARCH_ADDRESS` | 空 | 单值兜底 |
| `SEARCH_USERNAME` | 空 | ES 认证 |
| `SEARCH_PASSWORD` | 空 | ES 认证 |
| `SEARCH_ARTICLES_INDEX` | `picart_articles` | 文章索引名 |

### 2.2 基础设施 env（`internal/config/config.go:85-103`，**不属于本轮迁移范围**）

`SERVER_PORT`(3000) / `SERVER_MODE`(debug) / `POSTGRES_HOST`(127.0.0.1) / `POSTGRES_PORT`(5432) / `POSTGRES_USER` / `POSTGRES_PASSWORD` / `POSTGRES_DATABASE` / `POSTGRES_SSL_MODE`(disable) / `POSTGRES_LOG_LEVEL`(warn) / `REDIS_ADDR`(127.0.0.1:6379) / `REDIS_PASSWORD` / `REDIS_DB`(0) / `JWT_SECRET` / `JWT_EXPIRES_IN`(1h) / `JWT_REFRESH_EXPIRES_IN`(168h) / `JWT_ISSUER`(fantwo-core) / `LOG_LEVEL`(info) / `LOG_FORMAT`(console) / `SWAGGER_ENABLED`(false)

**这些永远留在 env**——它们是进程启动的前提（没有 DB 连接串就读不到 DB，循环依赖）。

### 2.3 迁移前的痛点（这就是为什么要迁）

1. **改任意一项都要改 env + 重启进程**。运维改个「上传大小上限」得走一次发布流程。
2. **`UPLOAD_COMPRESSION_ENABLED` 是假开关**：能改、能展示，但处理链路不读它——关了压缩，图照样被压缩。
3. **`/upload/config` 有 4 处硬编码**（水印模板 `{nickname}`、位置 `bottom-right`、透明度 `0.55`、压缩 `true`），与 env 值可能不一致。
4. **配置无审计**：谁在什么时候把上传上限从 20 改成 100，无从追溯。

---

## 三、迁移**后**的 env 配置

### 3.1 A 类：env 仍是**唯一通道**（改了必须重启）

| env 键 | 现状 | 为什么不能迁 |
|---|---|---|
| `STORAGE_DRIVER` | 只能 env，DB 里**没有**这个 key，且管理端 `POST/PATCH` 会 **400 拒绝** | 启动期决定建哪个 client |
| `S3_ENDPOINT` / `S3_BUCKET` / `S3_ACCESS_KEY` / `S3_SECRET_KEY` / `S3_REGION` / `S3_USE_SSL` / `S3_FORCE_PATH_STYLE` | 同上，7 项全部只能 env，管理端写入直接 400 | minio client 启动期创建，对象读写全走该 client。**迁进 DB 会产生假配置**（前端看到 B、实际写 A，删老文件还会删错桶） |
| `POSTGRES_*` / `REDIS_*` / `JWT_*` / `SERVER_*` / `LOG_*` / `SWAGGER_ENABLED` | 不变 | 进程启动前提 |

**这 9 项（storage_driver + S3 七项 + 基础设施）在数据库里查不到**，这是设计选择，不是遗漏。

### 3.2 B 类：env 变成**兜底默认值**（DB 优先，改 DB 不重启即生效）

| env 键 | 对应 DB key | 生效优先级 |
|---|---|---|
| `STORAGE_LOCAL_DIR` | `storage_local_dir` | **DB → env → `./uploads`** |
| `CDN_BASE_URL` | `cdn_base_url` | **DB → env → 空** |
| `UPLOAD_MAX_IMAGE_MB` | `upload_max_image_size` | **DB → env → 20** |
| `UPLOAD_MAX_VIDEO_MB` | `upload_max_video_size` | **DB → env → 500** |
| `UPLOAD_MAX_AUDIO_MB` | `upload_max_audio_size` | **DB → env → 50** |
| `UPLOAD_MAX_OTHER_MB` | `upload_max_other_size` | **DB → env → 100** |
| `UPLOAD_MAX_COUNT` | `upload_max_file_count` | **DB → env → 10** |
| `UPLOAD_COMPRESSION_ENABLED` | `upload_compression_enabled` | **DB → env → true**（且现在**真生效**） |
| `UPLOAD_THUMBNAIL_SPECS` | `upload_thumbnail_specs` | **DB（JSON）→ env → 四档** |
| `SEARCH_ENABLED` | `search_enabled` | **DB → env → false** |
| `SEARCH_ADDRESSES` / `SEARCH_ADDRESS` | `search_addresses` | **DB → env（两者并集）→ 空** |
| `SEARCH_USERNAME` | `search_username` | **DB → env → 空** |
| `SEARCH_PASSWORD` | `search_password` | **DB → env → 空**（DB 值管理端打码） |
| `SEARCH_ARTICLES_INDEX` | `search_articles_index` | **DB → env → `picart_articles`** |

**关键补充**：DB 值「为空串 / 解析不出东西」时会**回退到 env**，不会把 env 值抹掉。例如新库种子 `search_addresses = "[]"` 解析出空列表 → 回退 env 的 `SEARCH_ADDRESSES`，升级不会突然失去搜索。

### 3.3 C 类：迁移前就已 DB 优先（本轮只是统一范式）

| env 键 | DB key |
|---|---|
| `UPLOAD_WATERMARK_ENABLED` | `upload_watermark_enabled` |
| （无 env，纯 DB） | `upload_watermark_text_template` / `_position` / `_opacity_percent` / `_font_size` |

### 3.4 D 类：纯 DB，没有 env（原本就如此）

`site_*`（10） / `advertisement_*`（10） / `sensitive_words` / `content_audit_*` + provider 凭据（16）

### 3.5 一个真实的行为变化（要告知运维）

`.env.example` 里 `SEARCH_ENABLED=false`，但 DB 种子是 `search_enabled=true`。**DB 优先**，所以「开关值」现在是 true；不过 `Enabled() = enabled && 地址非空`，而地址列为空 → 回退 env → 也为空 → **搜索实际仍是关闭状态**。

结论：升级后搜索不会莫名开启，但**光看开关字段会以为开了**。判断搜索是否可用要看 `GET /search/status`。

---

## 四、数据库**现在**配置了什么

**56 条有效行**（`deleted_at = 0`），8 个分组。

```
$ SELECT "group", count(*) FROM config WHERE deleted_at=0 GROUP BY 1 ORDER BY 1;
 advertisement | 10
 audit         | 16
 cdn           |  1
 search        |  5
 site          | 10
 storage       |  1
 system        |  1
 upload        | 12
--------------------------------
 合计          | 56
```

### 4.1 `site`（10 条，全部 public）

| key | 值 |
|---|---|
| `site_name` | `PicArt` |
| `site_description` | `PicArt 图片社区` |
| `site_keywords` | `图片,社区,壁纸,插画` |
| `site_logo` / `site_favicon` / `site_icp` / `site_police_record` / `site_mail` / `site_contact` / `site_copyright` | 空 |

其中只有 `site_name` 被后端读（水印模板的 `{site}` 占位符）；其余 9 条经 `GET /config/public` 下发给前端。

### 4.2 `advertisement`（10 条，全部 public）

`advertisement_enabled=false`；`advertisement_home_banner` / `_home_banner_link` / `_article_top` / `_article_top_link` / `_article_bottom` / `_article_bottom_link` / `_article_sidebar` / `_article_sidebar_link` / `_global_notice` 全为空。后端不读，只经 `GET /config/advertisement` 下发。

### 4.3 `upload`（12 条，5 条 public）

| key | 值 | type | public | 后端读 |
|---|---|---|---|---|
| `upload_max_image_size` | `20` | number | ✓ | ✓ |
| `upload_max_video_size` | `500` | number | ✓ | ✓ |
| `upload_max_audio_size` | `50` | number | ✓ | ✓ |
| `upload_max_other_size` | `100` | number | ✓ | ✓ |
| `upload_max_file_count` | `10` | number | ✓ | ✓ |
| `upload_compression_enabled` | `true` | boolean | ✗ | ✓ |
| `upload_thumbnail_specs` | `[{"name":"thumb","size":200},{"name":"small","size":480},{"name":"medium","size":1024},{"name":"large","size":1920}]` | json | ✗ | ✓ |
| `upload_watermark_enabled` | `true` | boolean | ✗ | ✓ |
| `upload_watermark_text_template` | `{nickname}` | string | ✗ | ✓ |
| `upload_watermark_position` | `bottom-right` | string | ✗ | ✓ |
| `upload_watermark_opacity_percent` | `55` | number | ✗ | ✓ |
| `upload_watermark_font_size` | `0` | number | ✗ | ✓ |

### 4.4 `audit`（16 条，全部 public=false）

**审核行为 / 场景（8 条）**

| key | 值 | type |
|---|---|---|
| `content_audit_provider` | `none` | string |
| `content_audit_auto_block` | `true` | boolean |
| `content_audit_review_mode` | `auto` | string |
| `content_audit_sensitivity` | `medium` | string |
| `content_audit_image_enabled` | `false` | boolean |
| `content_audit_comment_enabled` | `false` | boolean |
| `content_audit_avatar_enabled` | `false` | boolean |
| `content_audit_article_enabled` | `false` | boolean |

**provider 凭据（8 条，其中 4 条打码）**

| key | 值 | 敏感打码 |
|---|---|---|
| `tencent_secret_id` | 空 | ✅ |
| `tencent_secret_key` | 空 | ✅ |
| `tencent_region` | `ap-beijing` | — |
| `tencent_text_biz_type` | 空 | — |
| `tencent_image_biz_type` | 空 | — |
| `aliyun_access_key_id` | 空 | ✅ |
| `aliyun_access_key_secret` | 空 | ✅ |
| `aliyun_region` | `cn-beijing` | — |

**为什么 4 个凭据都打码（含 `*_id`）**：`*Id` 语义上只是标识，但凭据类一律打码——避免将来有人把 secret 错填进 id 字段就泄露。判定用「白名单 + 命名兜底」双层，白名单是主渠道。

### 4.5 `search`（5 条，1 条 public）

| key | 值 | type | public |
|---|---|---|---|
| `search_enabled` | `true` | boolean | ✓ |
| `search_addresses` | `[]` | json | ✗ |
| `search_username` | 空 | string | ✗ |
| `search_password` | 空 | string | ✗（**打码**） |
| `search_articles_index` | `picart_articles` | string | ✗ |

### 4.6 `storage`（1 条）

`storage_local_dir = ./uploads`（string，非公开）

**注意**：`storage_driver` **不在**这里——它是启动期配置，选了不迁，且管理端创建会被 400 拒绝。

### 4.7 `cdn`（1 条，public）

`cdn_base_url` = 空（string）

### 4.8 `system`（1 条）

`sensitive_words = ["违禁词","敏感A","敏感B"]`（json，非公开，`/config/public` 强制剔除）

---

## 五、运维操作指南

| 想改什么 | 怎么改 | 生效时间 |
|---|---|---|
| 上传大小/数量上限、压缩开关、缩略图档位、水印全部参数、本地存储目录、CDN 域名 | `PATCH /config` 或 `PATCH /config/:id`（需 `setting:update` 权限） | **下一个请求即生效**（≤1 秒） |
| 搜索开关 / ES 地址 / 账号 / 索引名 | 同上 | ≤1 秒 + 下一次用到 ES 的请求（client 惰性重建） |
| 站点信息、广告位 | 同上（这些只影响前端展示） | 下一个请求 |
| 审核 provider / 场景开关 / 灵敏度 / 云厂商凭据 | 同上 | 下一个审核请求 |
| 敏感词 | `POST/DELETE /config/sensitive-words`（专用接口，勿直接 PATCH） | 即时（前缀树重建） |
| **存储驱动 / S3 七项** | 改 env（`STORAGE_DRIVER` / `S3_*`）+ **重启** | 重启后 |
| **DB / Redis / JWT / 端口 / 日志** | 改 env + 重启 | 重启后 |

**读接口的密钥保护**：`GET /config` 系列对敏感项返回 `value="***"` + `hasValue`（后端是否已配置）+ `masked=true`。传空串或 `***` 回写 = 不修改（库里原值保留）。前端可以把读到的对象原样回传。

**启动期配置的保护**：`GET /config` 里这类行（若历史数据存在）带 `startupOnly=true`，前端应置灰只读；即便前端漏判，`Create` / `Update` / `UpdateOne` / `UpdateAll` 四个入口都会 400 拒绝，文案会提示「改环境变量并重启」。

---

## 六、与上一版（42 条）的差异

| 变化 | 条数 |
|---|---|
| 上一版（T-34e 验收时） | 42 |
| 删 `content_audit_text_enabled` | −1（死配置：全仓无读取点，原仓库也没有这个 key） |
| 补审核行为 / 场景 | +7（provider / auto_block / review_mode / sensitivity / comment / avatar / article） |
| 补 provider 凭据 | +8（tencent 5 + aliyun 3） |
| **现在** | **56** |

算式（写在 `TestDefaultConfigSeedsCount` 注释里）：`35 − 1 + 14 − 7 + 15 = 56`

**为什么要补这 15 条**：`audit_service.go` 实际要读 16 个 key，但库里只有 1 条 → 其余 15 个全靠 `get(key, def)` 的硬编码默认值。这意味着运维「以为改了审核开关」，其实库里根本没那一行，改的是不存在的 key。补齐后：

- 审核开关可在管理端真改
- provider 凭据可填（4 条自动打码）
- `content_audit_sensitivity` 代码也补上了读取（原实现有此字段，我们之前漏读）

---

## 七、遗留与风险

| # | 项 | 说明 |
|---|---|---|
| 1 | **SQL 直改库有 ≤10 分钟延迟** | `configCacheTTL` 保持 10 分钟（用户已决策）。走 API 改立即失效缓存；SQL 直改要等过期 |
| 2 | **`.env.example` 未更新** | 里面的注释还写着「改这些 env 就行」，没说明「现在 DB 优先」。建议补一段说明，否则运维会继续改 env 然后发现重启也不生效（因为 DB 覆盖了） |
| 3 | **老库残留** | 若某环境曾手工插过 `content_audit_text_enabled` 或 `s3_*`，`EnsureSeeds` 只插不删，需一次性清理：`DELETE FROM config WHERE key='content_audit_text_enabled' OR key LIKE 's3_%' OR key='storage_driver'` |
| 4 | **`sensitivity` 读了但没接线** | 原实现也没接线（全仓库只有赋值，无消费点），我们只做到了「读进来」。将来接 provider 时要同步改 `queue.AliyunTextModeration` / `TencentTextModeration` 签名 |
| 5 | **S3 分支未真机验证** | 本机拉不到 minio 镜像（T-32 阻塞）。S3 相关只有单元层保证 |
| 6 | **`config` 表主键仍是 `character(36)`** | 其它表已 `varchar(36)`，属遗留。本次插入的 UUID 正好 36 字符未触发空格填充 |
| 7 | **审核 provider 凭据的打码回归测试** | 新增 4 个凭据已进白名单，但「PATCH 回写 `***` 不覆盖」这条针对新 4 项的显式用例尚未单独补（`TestConfigBuildUpdates` 覆盖的是 `s3_*` 与 `search_password`）。逻辑是通用的，但用例没点名这 4 个 |