package costqueryrealtime

import "os"

func removePath(p string) error {
	return os.Remove(p)
}

func renameSQLite(from, to string) error {
	return os.Rename(from, to)
}
