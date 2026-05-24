package filesystemcap

import "github.com/exclavenetwork/exclave-core/v5/common/platform/filesystem/fsifce"

type FileSystemCapabilitySet interface {
	OpenFileForReadSeek() fsifce.FileSeekerFunc
	OpenFileForRead() fsifce.FileReaderFunc
	OpenFileForWrite() fsifce.FileWriterFunc
	ReadDir() fsifce.FileReadDirFunc
	RemoveFile() fsifce.FileRemoveFunc
}
