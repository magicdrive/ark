package language

// HiddenDirReader is implemented by a provider whose files the index reads in
// a hidden directory (a name starting with "."), which it otherwise never
// enters (index.SkipDirName): the YAML provider reads .github, where GitHub
// keeps the workflows and actions it runs. In such a directory the index
// reads only the files of the providers that read it.
type HiddenDirReader interface {
	ReadsHiddenDir(name string) bool
}
