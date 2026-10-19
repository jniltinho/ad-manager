package agent

import "os"

// ownedByMe: the agent runs on Linux file servers only.
func ownedByMe(os.FileInfo) bool { return false }

func sameDevice(*os.File, *os.File) bool { return false }

func fileID(*os.File) string { return "" }
