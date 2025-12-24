# logd: Storage.Open ignores umask parameter

The umask parameter in Storage.Open() is documented but never used - directories are created with hardcoded 0755:

func Open(root string, umask int, logger *slog.Logger) (*Storage, error)

if err := os.MkdirAll(dir, 0755); err \!= nil {

Fix: Either use the umask parameter or remove it from the signature.