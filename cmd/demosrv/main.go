// Command demosrv runs an in-memory S3 server seeded with demo data for
// recording the README assets. Temporary tool — not shipped.
//
// The seed is deliberately shaped so every README scene has something
// meaningful to show: readable text files (content preview), a nested
// prefix tree (navigation, recursive search), distinct sizes/types
// (sorting, metadata) and a few buckets (bucket list).
package main

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/igungor/gofakes3"
	"github.com/igungor/gofakes3/backend/s3mem"
)

type seedObject struct {
	key         string
	body        string
	contentType string
}

const readme = `# lazys3

A terminal UI for S3-compatible object storage.

## Key bindings

  enter      open the highlighted bucket or prefix
  backspace  go up one level
  p          preview the highlighted file
  m          metadata for the highlighted item
  t          live transfers
  F          search the whole bucket
  ?          this help

## Notes

Prefixes behave like directories: entering one lists the keys
under it, and backspace walks back out. Nothing is cached, so a
refresh always shows what the bucket actually contains.

Presigned links are generated locally — the URL carries its own
signature and needs no credentials.

## Transfers

Downloads, uploads and directory syncs all run through the same
transfer engine, so they share progress reporting and
cancellation.
`

const releaseNotes = `Release 2026.1
==============

Highlights
----------
* per-profile start locations (skip ListBuckets)
* recursive search across a bucket
* multipart cleanup for interrupted uploads
* presigned upload URLs

Fixes
-----
* region is now sent with sync requests
* cancelled uploads abort their parts instead of leaking them
`

const hosts = `127.0.0.1   localhost
127.0.1.1   lazys3-demo
::1         localhost ip6-localhost ip6-loopback
`

func main() {
	backend := s3mem.New()
	faker := gofakes3.New(backend)

	seed := map[string][]seedObject{
		"documents": {
			{"README.md", readme, "text/markdown"},
			{"releases/2026.1/changelog.txt", releaseNotes, "text/plain"},
			{"releases/2026.1/checksums.txt", strings.Repeat("a1b2c3d4  lazys3-linux-amd64\n", 40), "text/plain"},
			{"releases/2025.4/changelog.txt", "Release 2025.4\n\nInitial public release.\n", "text/plain"},
			{"reports/2026/q1-report.csv", "quarter,revenue\nQ1,1200\n", "text/csv"},
			{"reports/2026/q2-report.csv", "quarter,revenue\nQ2,1450\n", "text/csv"},
			{"reports/2025/q4-report.csv", "quarter,revenue\nQ4,980\n", "text/csv"},
			{"notes/todo.md", "# todo\n\n- [x] ship v1\n- [ ] write docs\n", "text/markdown"},
		},
		"backups": {
			{"etc/hosts.bak", hosts, "text/plain"},
			{"etc/config.bak", "# app config\nlisten = :8080\n", "text/plain"},
			{"db/2026-10-01.sql", "-- nightly dump\nCREATE TABLE t (id int);\n", "application/sql"},
			{"db/2026-11-01.sql", "-- nightly dump\nCREATE TABLE t (id int);\n", "application/sql"},
		},
		"static-site": {
			{"index.html", "<!doctype html>\n<title>demo</title>\n<h1>hello</h1>\n", "text/html"},
			{"style.css", "body { font-family: monospace; }\n", "text/css"},
			{"logo.svg", "<svg xmlns=\"http://www.w3.org/2000/svg\"/>\n", "image/svg+xml"},
		},
	}

	for bucket, objs := range seed {
		if err := backend.CreateBucket(bucket); err != nil {
			panic(err)
		}
		for _, o := range objs {
			if _, err := backend.PutObject(bucket, o.key,
				map[string]string{"Content-Type": o.contentType},
				strings.NewReader(o.body), int64(len(o.body)), gofakes3.StorageStandard); err != nil {
				panic(err)
			}
		}
	}

	fmt.Println("demo S3 listening on 127.0.0.1:19093")
	log.Fatal(http.ListenAndServe("127.0.0.1:19093", faker.Server()))
}
