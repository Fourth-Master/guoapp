package core

import "testing"

func TestExtractPublishCandidates(t *testing.T) {
	page := `最新地址：<a href="https://abc.example.com">链接</a>
入口：https://abc.example.com
备用：https://def.example.com，备用2：http://ghi.example.com:8080
重复：https://abc.example.com/
无关：https://other.net/go
纯文本域名：jkl.example.com`

	// 空规则：提取全部链接域名，保序去重，去掉路径与端口归一。
	cases := []struct {
		name string
		rule string
		want []string
	}{
		{
			name: "空规则提取全部链接",
			rule: "",
			want: []string{"https://abc.example.com", "https://def.example.com", "http://ghi.example.com:8080", "https://other.net"},
		},
		{
			name: "后缀规则过滤",
			rule: ".example.com",
			want: []string{"https://abc.example.com", "https://def.example.com", "http://ghi.example.com:8080"},
		},
		{
			name: "正则捕获组",
			rule: `入口[:：]\s*(https?://[a-z0-9.-]+)`,
			want: []string{"https://abc.example.com"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := extractPublishCandidates(page, testCase.rule)
			if err != nil {
				t.Fatalf("提取失败: %v", err)
			}
			if len(got) != len(testCase.want) {
				t.Fatalf("候选数量不符: got %v want %v", got, testCase.want)
			}
			for index, host := range testCase.want {
				if got[index] != host {
					t.Fatalf("第 %d 个候选不符: got %v want %v", index, got, testCase.want)
				}
			}
		})
	}
}

func TestExtractPublishCandidatesInvalidRule(t *testing.T) {
	if _, err := extractPublishCandidates("page", "([invalid"); err == nil {
		t.Fatal("无效正则应当报错")
	}
}

func TestExtractPublishCandidatesBareDomain(t *testing.T) {
	got, err := extractPublishCandidates("最新域名 jkl.example.com 供访问", `最新域名\s+(\S+)\s+供访问`)
	if err != nil {
		t.Fatalf("提取失败: %v", err)
	}
	if len(got) != 1 || got[0] != "https://jkl.example.com" {
		t.Fatalf("纯文本域名应补全为 https 链接: %v", got)
	}
}
