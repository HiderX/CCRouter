package usage

import "os"

// alive cannot check processes here; a stale registry entry only delays a binding update.
func alive(pid int) bool { return pid > 0 }

func fileIdentity(fi os.FileInfo) string { return fi.Name() }
