package llmcloud

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
	"unsafe"

	"github.com/xmz14/lll/backend-go/internal/platform/filesystem"
	"golang.org/x/sys/windows"
)

type sessionVault struct{ path string }

func newSessionVault() *sessionVault {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil
	}
	return &sessionVault{path: filepath.Join(dir, "learn-with-ai", "cloud-session.dpapi")}
}

type savedSession struct {
	Endpoint string
	Session  session
	Selected Model
}

func (v *sessionVault) load() (session, Model) {
	encrypted, err := os.ReadFile(v.path)
	if err != nil || len(encrypted) == 0 || len(encrypted) > 128<<10 {
		return session{}, Model{}
	}
	input := windows.DataBlob{Size: uint32(len(encrypted)), Data: &encrypted[0]}
	var output windows.DataBlob
	if err := windows.CryptUnprotectData(&input, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &output); err != nil {
		return session{}, Model{}
	}
	defer windows.LocalFree(windows.Handle(uintptr(unsafe.Pointer(output.Data))))
	data := unsafe.Slice(output.Data, output.Size)
	defer clear(data)
	var saved savedSession
	if json.Unmarshal(data, &saved) != nil || saved.Endpoint != DefaultEndpoint ||
		saved.Session.Token == "" || saved.Session.Account.ID == "" || !time.Now().Before(saved.Session.ExpiresAt) {
		return session{}, Model{}
	}
	return saved.Session, saved.Selected
}

func (v *sessionVault) save(current session, selected Model) {
	if current.Token == "" {
		_ = os.Remove(v.path)
		return
	}
	data, err := json.Marshal(savedSession{DefaultEndpoint, current, selected})
	if err != nil {
		return
	}
	defer clear(data)
	input := windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
	var output windows.DataBlob
	if err := windows.CryptProtectData(&input, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &output); err != nil {
		return
	}
	defer windows.LocalFree(windows.Handle(uintptr(unsafe.Pointer(output.Data))))
	_ = filesystem.AtomicWriteFile(v.path, unsafe.Slice(output.Data, output.Size), 0600)
}
