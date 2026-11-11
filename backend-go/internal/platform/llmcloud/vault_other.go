//go:build !windows

package llmcloud

// Until an OS credential store is available, keep the account in memory.
type sessionVault struct{}

func newSessionVault() *sessionVault         { return nil }
func (*sessionVault) load() (session, Model) { return session{}, Model{} }
func (*sessionVault) save(session, Model)    {}
