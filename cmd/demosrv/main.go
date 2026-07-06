// Command demosrv runs an in-memory S3 server seeded with demo data for
// recording the README GIF. Temporary tool — not shipped.
package main

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/igungor/gofakes3"
	"github.com/igungor/gofakes3/backend/s3mem"
)

func main() {
	backend := s3mem.New()
	faker := gofakes3.New(backend)

	seed := map[string][]struct{ key, body string }{
		"documents": {
			{"reports/2026/q1-report.pdf", strings.Repeat("q1", 4000)},
			{"reports/2026/q2-report.pdf", strings.Repeat("q2", 6000)},
			{"notes.md", "# demo notes\nhello lazys3\n"},
			{"todo.txt", "ship it\n"},
		},
		"photos": {
			{"2026/summer/beach.jpg", strings.Repeat("img", 20000)},
			{"2026/summer/sunset.jpg", strings.Repeat("img", 15000)},
			{"avatar.png", strings.Repeat("p", 2048)},
		},
		"backups": {
			{"etc/hosts.bak", "127.0.0.1 localhost\n"},
		},
	}
	for bucket, objs := range seed {
		if err := backend.CreateBucket(bucket); err != nil {
			panic(err)
		}
		for _, o := range objs {
			if _, err := backend.PutObject(bucket, o.key,
				map[string]string{"Content-Type": "application/octet-stream"},
				strings.NewReader(o.body), int64(len(o.body)), gofakes3.StorageStandard); err != nil {
				panic(err)
			}
		}
	}
	fmt.Println("demo s3 on :19093")
	if err := http.ListenAndServe("127.0.0.1:19093", faker.Server()); err != nil {
		panic(err)
	}
}
