// 敏感词内存 Trie（Aho-Corasick 退化版：前缀树 + 逐起点扫描）。
//
// 选型说明（第三方优先结论）：Go 生态里的敏感词/Aho-Corasick 实现多为分词库
// （go-ego/gse、yanyiwu/gojieba）或玩具级 star 数极低的包，语义是「分词」而非
// 「多模式串精确命中」，且引入词表体积与初始化成本；本项目词量级在千条以内、
// 需要的是确定性匹配（含中文），60 行前缀树即可，且零依赖可审计——自写。
//
// 匹配语义对齐原 NestJS 敏感词过滤：子串命中即违规，大小写不敏感，
// 支持汉字（按 rune 建树）。
package infra

import (
	"strings"
	"sync"
)

// trieNode 前缀树节点。
type trieNode struct {
	children map[rune]*trieNode
	word     string // 非空表示从根到本节点构成一个敏感词
}

// Trie 敏感词前缀树，并发安全（读写锁 + 整体替换）。
type Trie struct {
	mu   sync.RWMutex
	root *trieNode
	size int
}

// NewTrie 由词表构建前缀树（自动去空白、去重、统一小写）。
func NewTrie(words []string) *Trie {
	t := &Trie{root: &trieNode{children: map[rune]*trieNode{}}}
	t.Rebuild(words)
	return t
}

// Rebuild 重建整棵树（词表变更时整体替换，避免增量删除的复杂状态）。
func (t *Trie) Rebuild(words []string) {
	root := &trieNode{children: map[rune]*trieNode{}}
	seen := make(map[string]struct{}, len(words))
	count := 0
	for _, raw := range words {
		w := strings.ToLower(strings.TrimSpace(raw))
		if w == "" {
			continue
		}
		if _, ok := seen[w]; ok {
			continue
		}
		seen[w] = struct{}{}
		node := root
		for _, r := range w {
			next, ok := node.children[r]
			if !ok {
				next = &trieNode{children: map[rune]*trieNode{}}
				node.children[r] = next
			}
			node = next
		}
		node.word = w
		count++
	}
	t.mu.Lock()
	t.root = root
	t.size = count
	t.mu.Unlock()
}

// Size 当前词条数。
func (t *Trie) Size() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.size
}

// Match 判断文本是否命中任一敏感词，返回首个命中词。
func (t *Trie) Match(text string) (string, bool) {
	t.mu.RLock()
	root := t.root
	t.mu.RUnlock()
	if root == nil || text == "" {
		return "", false
	}
	runes := []rune(strings.ToLower(text))
	for i := 0; i < len(runes); i++ {
		node := root
		for j := i; j < len(runes); j++ {
			next, ok := node.children[runes[j]]
			if !ok {
				break
			}
			node = next
			if node.word != "" {
				return node.word, true
			}
		}
	}
	return "", false
}

// MatchAll 返回文本命中的全部敏感词（去重，按词长降序）。
func (t *Trie) MatchAll(text string) []string {
	t.mu.RLock()
	root := t.root
	t.mu.RUnlock()
	if root == nil || text == "" {
		return nil
	}
	hits := map[string]struct{}{}
	runes := []rune(strings.ToLower(text))
	for i := 0; i < len(runes); i++ {
		node := root
		for j := i; j < len(runes); j++ {
			next, ok := node.children[runes[j]]
			if !ok {
				break
			}
			node = next
			if node.word != "" {
				hits[node.word] = struct{}{}
			}
		}
	}
	if len(hits) == 0 {
		return nil
	}
	out := make([]string, 0, len(hits))
	for w := range hits {
		out = append(out, w)
	}
	// 词长降序（长词优先，便于前端按最长匹配替换）
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && len([]rune(out[j])) > len([]rune(out[j-1])); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// Contains 便捷方法：是否违规。
func (t *Trie) Contains(text string) bool {
	_, ok := t.Match(text)
	return ok
}
