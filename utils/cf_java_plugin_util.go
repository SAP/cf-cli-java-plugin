package utils

type CfJavaPluginUtil interface {
	CheckRequiredTools(app string) (bool, error)
	GetAvailablePath(data string, userpath string) (string, error)
	CopyOverCat(args []string, src string, dest string) error
	DeleteRemoteFile(args []string, path string) error
	FindDumpFile(args []string, fullpath string, fspath string) (string, error)
}
