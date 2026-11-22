// Package errcode 错误码体系测试。
//
// 防的 bug：
//   - 错误码与 HTTP 状态不匹配：code=401 却返回 HTTP 200，前端拦截器读不到；
//   - With/Withf 修改了预定义值：共享的 *Err 被一处覆盖，全仓提示语串味；
//   - 业务细分码（1000+）与 HTTP 码段混用：前端按 code 分段判断会误判。
package errcode

import (
	"net/http"
	"testing"
)

// TestPredefined_HTTPAndCodeMatch 预定义错误码的 Code 与 HTTP 一致性。
//
// 约定（对齐原仓库 HttpExceptionFilter）：code 即 HTTP 状态码，
// 只有 1000+ 业务细分段是例外（映射到各自 HTTP 状态）。
func TestPredefined_HTTPAndCodeMatch(t *testing.T) {
	cases := []struct {
		name string
		e    *Err
		want int
	}{
		{"Success", Success, http.StatusOK},
		{"ErrBadRequest", ErrBadRequest, http.StatusBadRequest},
		{"ErrUnauthorized", ErrUnauthorized, http.StatusUnauthorized},
		{"ErrForbidden", ErrForbidden, http.StatusForbidden},
		{"ErrNotFound", ErrNotFound, http.StatusNotFound},
		{"ErrConflict", ErrConflict, http.StatusConflict},
		{"ErrTooManyRequests", ErrTooManyRequests, http.StatusTooManyRequests},
		{"ErrInternal", ErrInternal, http.StatusInternalServerError},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.e.Code != c.want {
				t.Errorf("Code：得到 %d，期望 %d", c.e.Code, c.want)
			}
			if c.e.HTTP != c.want {
				t.Errorf("HTTP：得到 %d，期望 %d（Code 与 HTTP 必须一致）", c.e.HTTP, c.want)
			}
			if c.e.Msg == "" {
				t.Error("Msg 为空：前端会显示空白提示")
			}
		})
	}
}

// TestBusinessCodes_MapToRealHTTPStatus 1000+ 业务细分码的 HTTP 映射。
//
// 防的是：细分码忘记设 HTTP（默认 0），response.Fail 会写出 HTTP 0 → 连接异常。
func TestBusinessCodes_MapToRealHTTPStatus(t *testing.T) {
	cases := []struct {
		name     string
		e        *Err
		wantHTTP int
	}{
		{"ErrUserAlreadyExists", ErrUserAlreadyExists, http.StatusConflict},
		{"ErrBadCredentials", ErrBadCredentials, http.StatusUnauthorized},
		{"ErrAccountBanned", ErrAccountBanned, http.StatusForbidden},
		{"ErrTokenExpired", ErrTokenExpired, http.StatusUnauthorized},
		{"ErrInsufficientWallet", ErrInsufficientWallet, http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.e.Code < 1000 {
				t.Errorf("业务细分码应 >= 1000，得到 %d", c.e.Code)
			}
			if c.e.HTTP != c.wantHTTP {
				t.Errorf("HTTP：得到 %d，期望 %d", c.e.HTTP, c.wantHTTP)
			}
			if c.e.HTTP < 100 || c.e.HTTP > 599 {
				t.Errorf("HTTP 状态 %d 非法（会导致响应写入失败）", c.e.HTTP)
			}
		})
	}
}

// TestAllPredefined_HaveValidHTTP 遍历全部预定义码，确保 HTTP 都合法。
// 用切片登记，新增码时忘记加进本测试也不会漏 —— 只要在下面表里补一行即可。
func TestAllPredefined_HaveValidHTTP(t *testing.T) {
	all := []*Err{
		Success,
		ErrBadRequest, ErrUnauthorized, ErrForbidden, ErrNotFound,
		ErrConflict, ErrTooManyRequests, ErrInternal,
		ErrUserAlreadyExists, ErrBadCredentials, ErrAccountBanned,
		ErrTokenExpired, ErrInsufficientWallet,
	}
	for _, e := range all {
		if e == nil {
			t.Fatal("预定义码中存在 nil（引用即 panic）")
		}
		if e.HTTP < 100 || e.HTTP > 599 {
			t.Errorf("code=%d 的 HTTP=%d 非法", e.Code, e.HTTP)
		}
		if e.Msg == "" {
			t.Errorf("code=%d 的 Msg 为空", e.Code)
		}
	}
}

// TestWith_DoesNotMutateOriginal 覆盖提示语不得污染预定义值。
//
// 这是最容易被忽略的坑：Err 是包级共享指针，若 With 直接改 e.Msg，
// 一次调用之后全仓所有该错误的提示语都变了（并发下还会 data race）。
func TestWith_DoesNotMutateOriginal(t *testing.T) {
	original := ErrBadRequest.Msg
	custom := ErrBadRequest.With("自定义提示")

	if custom.Msg != "自定义提示" {
		t.Errorf("With 未生效：得到 %q", custom.Msg)
	}
	if ErrBadRequest.Msg != original {
		t.Errorf("With 污染了预定义值：%q → %q（全仓提示语会串味）",
			original, ErrBadRequest.Msg)
	}
	if custom == ErrBadRequest {
		t.Error("With 返回了同一个指针（应为新对象）")
	}
	// Code/HTTP 必须继承
	if custom.Code != ErrBadRequest.Code || custom.HTTP != ErrBadRequest.HTTP {
		t.Errorf("With 丢失了 Code/HTTP：得到 code=%d http=%d", custom.Code, custom.HTTP)
	}
}

// TestWithf_Formatting 格式化覆盖。
func TestWithf_Formatting(t *testing.T) {
	e := ErrNotFound.Withf("文章 %s 不存在", "a-1")
	if e.Msg != "文章 a-1 不存在" {
		t.Errorf("Withf 格式化失败：得到 %q", e.Msg)
	}
	if ErrNotFound.Msg != "资源不存在" {
		t.Errorf("Withf 污染了预定义值：%q", ErrNotFound.Msg)
	}
}

// TestWith_ChainIndependent 连续 With 之间互不影响。
func TestWith_ChainIndependent(t *testing.T) {
	a := ErrBadRequest.With("A")
	b := ErrBadRequest.With("B")
	if a.Msg != "A" || b.Msg != "B" {
		t.Errorf("连续 With 互相干扰：a=%q b=%q", a.Msg, b.Msg)
	}
	if a == b {
		t.Error("两次 With 返回同一指针")
	}
}

// TestError_ImplementsError interface 实现（多处用于 err.(*errcode.Err) 断言）。
func TestError_ImplementsError(t *testing.T) {
	var err error = ErrNotFound
	if err.Error() != ErrNotFound.Msg {
		t.Errorf("Error() 应返回 Msg：得到 %q，期望 %q", err.Error(), ErrNotFound.Msg)
	}

	e := ErrNotFound.With("自定义")
	err = e
	if err.Error() != "自定义" {
		t.Errorf("覆盖后 Error() 应返回新提示：得到 %q", err.Error())
	}
}

// TestTypeAssertion 类型断言（handler 里靠它区分业务错误与普通 error）。
func TestTypeAssertion(t *testing.T) {
	var err error = ErrForbidden
	e, ok := err.(*Err)
	if !ok {
		t.Fatal("Err 无法断言回 *Err（handler 的错误分支会走错）")
	}
	if e.HTTP != http.StatusForbidden {
		t.Errorf("HTTP：得到 %d", e.HTTP)
	}
}
