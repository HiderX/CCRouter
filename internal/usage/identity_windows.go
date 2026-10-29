package usage

import "os"

func fileIdentity(fi os.FileInfo) string { return fi.Name() }
