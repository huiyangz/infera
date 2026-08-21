package flow

import "testing"

// TestDefaultMergePolicy：默认档位是手动合并（人点合并卡）。
func TestDefaultMergePolicy(t *testing.T) {
	p := DefaultMergePolicy()
	if p.Mode != MergeManual {
		t.Fatalf("默认合并策略 = %q, want manual", p.Mode)
	}
	if p.DiffLineThreshold != 0 {
		t.Fatalf("手动档不携带阈值, got %d", p.DiffLineThreshold)
	}
}

// TestMergePolicyValidate：threshold 档必须带正阈值；其余档位阈值必须为零
// （档位语义不混淆）。
func TestMergePolicyValidate(t *testing.T) {
	cases := []struct {
		name string
		pol  MergePolicy
		want bool
	}{
		{"手动档", MergePolicy{Mode: MergeManual}, true},
		{"自动档", MergePolicy{Mode: MergeAutoPass}, true},
		{"阈值档带阈值", MergePolicy{Mode: MergeThreshold, DiffLineThreshold: 400}, true},
		{"阈值档无阈值", MergePolicy{Mode: MergeThreshold}, false},
		{"阈值档零阈值", MergePolicy{Mode: MergeThreshold, DiffLineThreshold: 0}, false},
		{"阈值档负阈值", MergePolicy{Mode: MergeThreshold, DiffLineThreshold: -1}, false},
		{"未知档位", MergePolicy{Mode: MergePolicyMode("yolo")}, false},
		{"手动档带阈值语义混淆", MergePolicy{Mode: MergeManual, DiffLineThreshold: 10}, false},
		{"空档位", MergePolicy{Mode: ""}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.pol.Validate() == nil; got != tc.want {
				t.Fatalf("Validate(%+v) ok = %v, want %v", tc.pol, got, tc.want)
			}
		})
	}
}
