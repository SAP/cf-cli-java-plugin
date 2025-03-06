package utils

type CfJavaPluginUtil interface {
	CheckRequiredTools(app string) (bool, error)
	GetAvailablePath(data string, userpath string) (string, error)
	CopyOverCat(args []string, src string, dest string) error
	DeleteRemoteFile(args []string, path string) error
	FindHeapDumpFile(args []string, fullpath string, fspath string) (string, error)
	FindJFRFile(args []string, fullpath string, fspath string) (string, error)
	FindFile(args []string, fullpath string, fspath string, pattern string) (string, error)
}
