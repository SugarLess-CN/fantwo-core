# 水印字体授权说明（watermark.otf）

## 字体来源

- **字体名**：Noto Sans SC（思源/Noto 系列简体中文黑体）
- **原始文件**：`NotoSansSC-Regular.otf`（未子集化原文件 8.1 MB，来源为本机系统已安装的 Noto Sans SC）
- **上游项目**：Google Noto Fonts / Adobe Source Han Sans（同源设计）
- **版权声明（原文，取自字体 name 表 ID 0）**：
  > © 2014-2020 Adobe (http://www.adobe.com/).
- **许可**：**SIL Open Font License, Version 1.1**（OFL-1.1）
  - 许可全文：https://scripts.sil.org/OFL
  - 字体 name 表 ID 13 原文：*This Font Software is licensed under the SIL Open Font License, Version 1.1. This Font Software is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.*
  - 字体 name 表 ID 14：http://scripts.sil.org/OFL

## 本项目对字体做的处理（OFL §1 允许的修改与再分发）

- **子集化（subsetting）**：用 fontTools `pyftsubset` 从原字体抽取本项目实际需要的字符集，
  文件体积 8.1 MB → **1.6 MB**。保留字符范围：
  - ASCII 可打印字符（U+0020–U+007E）
  - 常用中文标点
  - GB2312 一级 + 二级全部汉字（6763 字）
  - 合计 **6891 个字符**
- 子集化参数：`--layout-features=*`、`--no-hinting`、`--desubroutinize`、`--recalc-bounds`、丢弃 DSIG 表
- **未修改**：字形轮廓设计、字体名称（family/subfamily 保留 `Noto Sans SC` / `Regular`）、`name` 表中的版权行与许可声明（OFL 要求的版权声明原样保留）

## OFL-1.1 合规要点（与本项目相关的部分）

1. **允许**在软件中嵌入、打包、再分发字体（含子集），无需支付费用；
2. **必须**随字体一起保留上述版权声明与本许可声明（即本文件）；
3. **允许**对字体做修改（子集化即修改），但修改后的字体**不得**使用 Reserved Font Name
   ——Noto Sans SC 未声明 Reserved Font Name，故子集文件沿用原名是允许的；
4. **禁止**单独售卖字体文件本身（本项目是随服务端程序一起分发，属于允许情形）；
5. 字体按「AS IS」提供，无任何担保。

## 使用位置

- 编译期通过 `//go:embed assets/watermark.otf` 嵌入到 `internal/service/infra`
  （见 `watermark.go`），仅在服务端为上传图片绘制文字水印时使用，不对外暴露字体文件。
- 运行时从 config 表读取水印参数（模板 / 位置 / 透明度 / 字号），见
  `model.DefaultConfigSeeds()` 中的 `upload_watermark_*` 项。
