// Package speech invokes only system-local speech recognition. No cloud fallback.
package speech

import (
	"context"
	"encoding/binary"
	"errors"
	"runtime"
	"strings"
	"time"
)

type Capability struct {
	Platform  string   `json:"platform"`
	Languages []string `json:"languages"`
	Available bool     `json:"available"`
	SetupHint string   `json:"setupHint"`
}

type Result struct {
	Languages  []string `json:"languages"`
	Text       string   `json:"text"`
	Error      string   `json:"error"`
	Confidence float64  `json:"confidence"`
}

func ValidLanguage(language string) bool {
	return language == "auto" || language == "zh-CN" || language == "en-US"
}

func Probe(ctx context.Context) Capability {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	result, err := run(ctx, "probe", "", nil)
	cap := Capability{Platform: runtime.GOOS, Languages: result.Languages, Available: err == nil && len(result.Languages) > 0, SetupHint: "请在系统设置中安装中文或英文语音识别语言，并允许本应用使用语音识别。音频仅在本机转写。"}
	if runtime.GOOS == "linux" {
		cap.SetupHint = "当前 Linux 没有可调用的系统自带语音识别服务，请使用文字输入。"
	}
	return cap
}

func Recognize(ctx context.Context, language string, data []byte) (string, error) {
	if !ValidLanguage(language) {
		return "", errors.New("语音目前只支持中文和英文")
	}
	if err := ValidateWave(data); err != nil {
		return "", err
	}
	if silentWave(data) {
		return "", nil
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	var result Result
	var err error
	if language == "auto" {
		result, err = recognizeAuto(ctx, data, run)
	} else {
		result, err = run(ctx, "recognize", language, data)
	}
	if ctx.Err() != nil {
		return "", errors.New("语音解析超时或已取消，请重试")
	}
	if err != nil {
		return "", errors.New("系统语音识别不可用，请检查语言安装与语音权限")
	}
	switch result.Error {
	case "language-unavailable":
		return "", errors.New("系统未安装所选语言的本地语音识别，请在系统语言设置中安装中文或英文语音功能")
	case "permission-denied":
		return "", errors.New("系统未允许语音识别，请在隐私设置中授权后重试")
	case "on-device-unavailable":
		return "", errors.New("系统不支持所选语言的本地语音识别，本应用不会改用云端识别")
	case "timeout":
		return "", errors.New("语音解析超时，请缩短录音后重试")
	case "":
		return result.Text, nil
	default:
		return "", errors.New("系统语音解析失败，请检查音频或重试")
	}
}

// Native engines are language-specific. Compare their local hypotheses without
// sending audio to a model service. This selects a dominant language, not a
// guarantee of accurate code-switching within a sentence.
func recognizeAuto(ctx context.Context, data []byte, runner func(context.Context, string, string, []byte) (Result, error)) (Result, error) {
	capability, err := runner(ctx, "probe", "", nil)
	if err != nil {
		return Result{}, err
	}
	type candidate struct {
		result Result
		err    error
	}
	var languages []string
	for _, language := range []string{"zh-CN", "en-US"} {
		for _, installed := range capability.Languages {
			if installed == language {
				languages = append(languages, language)
				break
			}
		}
	}
	if len(languages) == 0 {
		return Result{Error: "language-unavailable"}, nil
	}
	results := make([]chan candidate, len(languages))
	for i, language := range languages {
		results[i] = make(chan candidate, 1)
		go func(ch chan candidate, language string) {
			r, e := runner(ctx, "recognize", language, data)
			ch <- candidate{r, e}
		}(results[i], language)
	}
	best := Result{}
	var failure candidate
	succeeded := false
	for _, ch := range results {
		select {
		case <-ctx.Done():
			return Result{}, ctx.Err()
		case c := <-ch:
			if c.err != nil || c.result.Error != "" {
				failure = c
				continue
			}
			succeeded = true
			if strings.TrimSpace(c.result.Text) != "" && (best.Text == "" || c.result.Confidence > best.Confidence) {
				best = c.result
			}
		}
	}
	if succeeded {
		return best, nil
	}
	return failure.result, failure.err
}

// Exact digital silence is an empty input, independent of installed recognizers.
func silentWave(data []byte) bool {
	for offset := 12; offset+8 <= len(data); {
		size := int(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		start := offset + 8
		if string(data[offset:offset+4]) == "data" {
			for _, value := range data[start : start+size] {
				if value != 0 {
					return false
				}
			}
		}
		offset = start + size + (size % 2)
	}
	return true
}

// ValidateWave bounds both decoded duration and format before invoking a native service.
func ValidateWave(data []byte) error {
	invalid := errors.New("请上传不超过60秒的16kHz单声道PCM WAV音频，或使用录音按钮")
	if len(data) < 44 || len(data) > 20<<20 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" || uint64(binary.LittleEndian.Uint32(data[4:8]))+8 != uint64(len(data)) {
		return invalid
	}
	formatOK := false
	samples := 0
	for offset := 12; offset+8 <= len(data); {
		size := int(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		start := offset + 8
		if size < 0 || size > len(data)-start {
			return invalid
		}
		switch string(data[offset : offset+4]) {
		case "fmt ":
			if size < 16 {
				return invalid
			}
			f := data[start : start+size]
			formatOK = binary.LittleEndian.Uint16(f) == 1 && binary.LittleEndian.Uint16(f[2:]) == 1 && binary.LittleEndian.Uint32(f[4:]) == 16000 && binary.LittleEndian.Uint32(f[8:]) == 32000 && binary.LittleEndian.Uint16(f[12:]) == 2 && binary.LittleEndian.Uint16(f[14:]) == 16
		case "data":
			samples += size
		}
		offset = start + size + (size % 2)
	}
	if !formatOK || samples == 0 || samples%2 != 0 || samples > 60*32000 {
		return invalid
	}
	return nil
}
