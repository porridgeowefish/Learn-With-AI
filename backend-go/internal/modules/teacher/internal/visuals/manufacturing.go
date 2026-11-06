package visuals

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"sync"
	"time"
)

// These are verified Commons file names, not image URLs or bundled artwork.
// Each lookup revalidates current identity, authorship and license through the
// same provider boundary used by diagram search. Missing files stay missing.
type manufacturingStage struct {
	id, title, caption string
	files, keywords    []string
}

var semiconductorStages = []manufacturingStage{
	{"substrate", "晶圆起点", "观察单晶硅生长装置；此图不包含完整的切片、抛光与清洗。", []string{"Czochralski Process.svg"}, []string{"晶圆制备", "晶体生长", "单晶", "czochralski", "crystal growth", "wafer preparation"}},
	{"patterning", "前道：光刻与图形转移", "观察光刻胶、掩模和被加工材料的层次，区分曝光与后续刻蚀。", []string{"Photolithography etching process.svg"}, []string{"光刻", "曝光", "掩模", "photolithography", "lithography", "photomask"}},
	{"film-etch", "前道：薄膜沉积与刻蚀", "对照材料的添加与去除；这些步骤会反复发生，不能理解为只经过一次的直线产线。", []string{"Sequence during CVD (en).svg", "Etching wet-chemical vs rie (EN).svg"}, []string{"沉积", "薄膜", "刻蚀", "蚀刻", "deposition", "etching", "cvd", "thin film"}},
	{"packaging", "后道：互连与封装", "比较引线键合和倒装互连两种示意；不是同一芯片必须依次采用的工序。", []string{"Ball-wedge sequence.svg", "Flip chip side-view.svg"}, []string{"封装", "键合", "倒装", "packaging", "bonding", "flip chip"}},
	{"testing", "检测与测试", "区分制程量测、晶圆电测与封装后测试；当前目录尚无经核验的测试设备SVG。", nil, []string{"检测", "测试", "量测", "探针", "inspection", "metrology", "testing", "test", "probe"}},
	{"handling", "厂内搬运：机械臂", "SCARA仅用于解释关节与平面运动，不代表晶圆厂专用机械臂、洁净等级或完整AMHS布局。", []string{"SCARA manipulator representation.svg"}, []string{"搬运", "传送", "机器人", "机械臂", "handling", "transport", "robot", "scara", "amhs"}},
}

var automationStages = []manufacturingStage{
	{"handling", "执行机构：机械臂", "从SCARA关节和工作空间理解取放动作，不能据此推定任意产线的设备型号。", []string{"SCARA manipulator representation.svg"}, []string{"机械臂", "机器人", "robot", "scara"}},
	{"conveying", "输送与定位", "输送线上推杆居中装置的透视和运动学示意，可用于讨论工件定位与节拍。", []string{"Adherence colis modele bacpro msma 2001.svg"}, []string{"输送", "传送", "定位", "conveyor", "conveying", "positioning"}},
	{"feedback", "检测与反馈控制", "通用闭环框图说明测量、比较和执行的关系，不是特定传感器的安装图。", []string{"General closed loop feedback system.svg"}, []string{"闭环", "反馈", "控制", "feedback", "control", "sensor"}},
	{"supervision", "生产控制层级", "自动化层级图帮助区分现场设备、控制、MES与企业管理；图中原始标注为德文。", []string{"Automatisierungspyramide MES.svg"}, []string{"mes", "plc", "层级", "自动化", "automation", "supervision"}},
}

type FigureGroup struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Caption string   `json:"caption"`
	Figures []Figure `json:"figures"`
}

type FigureGap struct {
	Stage  string `json:"stage"`
	Reason string `json:"reason"`
}

type ManufacturingResult struct {
	Topic    string        `json:"topic"`
	Scope    string        `json:"scope"`
	Overview string        `json:"overview"`
	Groups   []FigureGroup `json:"groups"`
	Gaps     []FigureGap   `json:"gaps"`
}

type catalogEntry struct {
	figures []Figure
	created time.Time
}

// Only the two built-in catalogs are cached. Incomplete/error responses are not
// cached, and callers always receive separate slices before budget trimming.
var catalogCache = struct {
	sync.Mutex
	entries map[string]catalogEntry
}{entries: map[string]catalogEntry{}}

func manufacturingPlan(query string) (string, []manufacturingStage, []manufacturingStage) {
	q := strings.ToLower(query)
	stages, scope := automationStages, "automation"
	if containsKeyword(q, []string{"芯片", "晶圆", "半导体", "光刻", "刻蚀", "沉积", "薄膜", "封装", "键合", "倒装", "chip", "semiconductor", "wafer", "lithography", "czochralski", "deposition", "etching", "bonding", "packaging"}) {
		stages, scope = semiconductorStages, "semiconductor"
	}
	// Broad process questions retain context even when a stage keyword is used.
	if containsKeyword(q, []string{"全流程", "流水线", "产线", "生产线", "智能制造", "全景", "overview", "production line", "manufacturing process", "smart manufactur", "smart factory"}) {
		return scope, stages, stages
	}
	var selected []manufacturingStage
	for _, stage := range stages {
		if containsKeyword(q, stage.keywords) {
			selected = append(selected, stage)
		}
	}
	if len(selected) == 0 {
		selected = stages
	}
	return scope, stages, selected
}

func containsKeyword(query string, keywords []string) bool {
	for _, keyword := range keywords {
		if strings.Contains(query, keyword) {
			return true
		}
	}
	return false
}

// SearchManufacturing retrieves a curated, online-verified group of structural
// figures. It performs at most one metadata request (15s), never a web crawl.
// A focused topic selects relevant groups; other topics get an explicitly
// generic automation guide and can be refined through ordinary diagram search.
func SearchManufacturing(ctx context.Context, query string) (ManufacturingResult, error) {
	query = strings.TrimSpace(query)
	if query == "" || len([]rune(query)) > 120 {
		return ManufacturingResult{}, errors.New("query must contain 1–120 characters")
	}
	scope, catalog, selected := manufacturingPlan(query)
	result := ManufacturingResult{Topic: query, Scope: scope, Groups: []FigureGroup{}, Gaps: []FigureGap{}}
	result.Overview = "公共图库在线核验的通用自动化图示目录，不是该主题或某一工厂的完整产线。可用diagram继续检索具体部件。"
	if scope == "semiconductor" {
		result.Overview = "半导体导读图组：区分前道晶圆加工、后道封装测试与厂内搬运。制程会反复循环；各图来自不同作者，不代表同一工厂的真实产线、完整工艺或设备选型。"
	}
	figures, err := manufacturingCatalog(ctx, scope, catalog)
	if err != nil && ctx.Err() != nil {
		return result, ctx.Err()
	}
	byTitle := map[string]Figure{}
	for _, figure := range figures {
		byTitle[figure.Title] = figure
	}
	seen := map[string]bool{}
	for _, stage := range selected {
		group := FigureGroup{ID: stage.id, Title: stage.title, Caption: stage.caption, Figures: []Figure{}}
		missing := false
		for _, file := range stage.files {
			figure, ok := byTitle[file]
			if !ok {
				missing = true
				continue
			}
			key := figure.Provider + ":" + figure.Asset
			if !seen[key] {
				group.Figures = append(group.Figures, figure)
				seen[key] = true
			}
		}
		result.Groups = append(result.Groups, group)
		switch {
		case len(stage.files) == 0:
			result.Gaps = append(result.Gaps, FigureGap{stage.id, "目录暂无经核验的对应SVG；可用文字或Mermaid解释流程，不编造设备图。"})
		case err != nil:
			result.Gaps = append(result.Gaps, FigureGap{stage.id, "公共图库暂不可用，本阶段未取得可信图示。"})
		case missing:
			result.Gaps = append(result.Gaps, FigureGap{stage.id, "部分图示缺失或未通过来源、格式、署名和许可核验。"})
		}
	}
	return result, nil
}

func manufacturingCatalog(ctx context.Context, scope string, stages []manufacturingStage) ([]Figure, error) {
	catalogCache.Lock()
	entry, ok := catalogCache.entries[scope]
	catalogCache.Unlock()
	if ok && time.Since(entry.created) < 15*time.Minute {
		return append([]Figure(nil), entry.figures...), nil
	}
	var titles []string
	seen := map[string]bool{}
	for _, stage := range stages {
		for _, file := range stage.files {
			if !seen[file] {
				titles = append(titles, "File:"+file)
				seen[file] = true
			}
		}
	}
	if len(titles) == 0 || len(titles) > 8 {
		return nil, errors.New("invalid manufacturing catalog size")
	}
	lookup, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	params := url.Values{"action": {"query"}, "format": {"json"}, "titles": {strings.Join(titles, "|")}, "prop": {"imageinfo"}, "iiprop": {"url|mime|extmetadata"}, "iiextmetadatafilter": {"Artist|LicenseShortName|LicenseUrl|Restrictions"}}
	figures, err := commonsFiguresLimit(lookup, params, 8)
	if err != nil {
		return nil, err
	}
	verified := make([]Figure, 0, len(figures))
	for _, figure := range figures {
		if seen[figure.Title] {
			verified = append(verified, figure)
		}
	}
	if len(verified) == len(titles) {
		catalogCache.Lock()
		catalogCache.entries[scope] = catalogEntry{append([]Figure(nil), verified...), time.Now()}
		catalogCache.Unlock()
	}
	return verified, nil
}
