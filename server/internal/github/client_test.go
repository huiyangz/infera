package github

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNew：构造期挡掉误配（对齐 tasksource.New 的入口防线风格）——
// token 缺失、BaseURL 非法都在构造期报错，不漏到运行期变难排查的 401/404。
func TestNew(t *testing.T) {
	t.Run("token 缺失报错", func(t *testing.T) {
		_, err := New("")
		require.ErrorContains(t, err, "Token")
	})

	t.Run("token 存在即可构造", func(t *testing.T) {
		c, err := New("ghp_t")
		require.NoError(t, err)
		require.NotNil(t, c)
	})

	t.Run("WithBaseURL 非法地址报错", func(t *testing.T) {
		_, err := New("ghp_t", WithBaseURL("://not-a-url"))
		require.ErrorContains(t, err, "BaseURL")
	})

	t.Run("WithBaseURL 非法 scheme 报错", func(t *testing.T) {
		_, err := New("ghp_t", WithBaseURL("ftp://github.example.com"))
		require.ErrorContains(t, err, "http(s)")
	})
}
