package i18n

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"testing"
)

// keyConstantsFromSource 用 AST 解析**全部** `keys*.go`，提取所有 `Xxx Key = "yyy"` 常量。
//
// 为什么必须用 AST 而不是遍历 enCatalog：
// 常量在运行时无法枚举，而 `Keys()` 是从 enCatalog 反向收集的 ——
// 用它做「每个 key 都有文案」的检查是同义反复（遍历 enCatalog 查 enCatalog，永远通过）。
// 历史教训：`AuthOAuthFailedDetail` 只加了 key 常量、忘了加文案，
// 旧测试一直是绿的，线上会返回裸 key 字符串。
//
// 为什么必须遍历所有 keys*.go 而不是只解析 keys.go：
// 常量按域拆在 5 个文件里（keys / keys_content / keys_message / keys_commerce / keys_extra），
// 只解析一个文件会把其余文件的常量误判成「孤儿文案」（本测试第一版就犯过这个错）。
func keyConstantsFromSource(t *testing.T) []Key {
	t.Helper()
	files, err := filepath.Glob("keys*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("未找到 keys*.go: %v", err)
	}
	var out []Key
	fset := token.NewFileSet()
	for _, name := range files {
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("解析 %s 失败: %v", name, err)
		}
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				typeName, ok := vs.Type.(*ast.Ident)
				if !ok || typeName.Name != "Key" {
					continue
				}
				for _, v := range vs.Values {
					lit, ok := v.(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					s, err := strconv.Unquote(lit.Value)
					if err != nil {
						continue
					}
					out = append(out, Key(s))
				}
			}
		}
	}
	return out
}

// TestEveryKeyConstantHasText 每个 Key 常量都必须在英文目录里有文案。
//
// 这是防「加 key 忘文案」的真正守卫（旧 TestCatalogComplete 是同义反复）。
func TestEveryKeyConstantHasText(t *testing.T) {
	declared := keyConstantsFromSource(t)
	if len(declared) == 0 {
		t.Fatal("没从 keys.go 解析出任何 Key 常量 —— AST 解析逻辑失效，检查测试本身")
	}
	missing := 0
	for _, k := range declared {
		if v, ok := enCatalog[k]; !ok {
			t.Errorf("key 常量 %q 在 enCatalog 里没有文案（线上会返回裸 key）", k)
			missing++
		} else if v == "" {
			t.Errorf("key 常量 %q 的文案是空串", k)
			missing++
		}
	}
	if missing == 0 {
		t.Logf("✓ %d 个 Key 常量全部有文案", len(declared))
	}
}

// TestNoOrphanCatalogEntry 反向检查：enCatalog 里不该有 keys.go 未声明的 key。
//
// 防的是「删了 key 常量、文案忘删」→ 死代码堆积，且让人误以为那个 key 还在用。
func TestNoOrphanCatalogEntry(t *testing.T) {
	declared := make(map[Key]bool, 256)
	for _, k := range keyConstantsFromSource(t) {
		declared[k] = true
	}
	orphan := 0
	for k := range enCatalog {
		if !declared[k] {
			t.Errorf("enCatalog 里的 %q 在 keys.go 中没有对应常量（孤儿文案）", k)
			orphan++
		}
	}
	if orphan == 0 {
		t.Logf("✓ 无孤儿文案")
	}
}
