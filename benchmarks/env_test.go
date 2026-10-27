package benchmarks_test

import "os"

func benchEnabled() bool { return os.Getenv("ATLAS_BENCH") == "1" }
