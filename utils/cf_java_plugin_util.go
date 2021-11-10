package utils

type CfJavaPluginUtil interface {
	CheckRequiredTools(app string) (bool, error)
	GetAvailablePath(data string, userpath string) (string, error)
	CopyOverCat(app string, src string, dest string) error
	DeleteRemoteFile(app string, path string) error
	FindDumpFile(app string, fullpath string, fspath string) (string, error)
}
