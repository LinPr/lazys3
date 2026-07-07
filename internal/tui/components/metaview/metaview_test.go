package metaview

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/charmbracelet/x/ansi"
)

// rowValue returns the value of the first row with the given key ("" when
// absent).
func rowValue(rows []Row, key string) string {
	for _, r := range rows {
		if r.Key == key {
			return r.Value
		}
	}
	return ""
}

func hasKey(rows []Row, key string) bool {
	for _, r := range rows {
		if r.Key == key {
			return true
		}
	}
	return false
}

func TestObjectRowsRenderPopulatedFieldsAndOmitEmpties(t *testing.T) {
	mod := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	head := &s3.HeadObjectOutput{
		ContentLength:        aws.Int64(1234),
		ContentType:          aws.String("text/plain"),
		ETag:                 aws.String(`"abc123"`),
		LastModified:         &mod,
		StorageClass:         s3types.StorageClassStandardIa,
		VersionId:            aws.String("v1"),
		CacheControl:         aws.String("max-age=60"),
		ContentEncoding:      aws.String("gzip"),
		ServerSideEncryption: s3types.ServerSideEncryptionAwsKms,
		SSEKMSKeyId:          aws.String("kms-key-1"),
		ChecksumSHA256:       aws.String("deadbeef"),
		Metadata:             map[string]string{"owner": "lin", "app": "lazys3"},
	}
	rows := objectRows("dir/file.txt", head)

	if got := rowValue(rows, "Key"); got != "dir/file.txt" {
		t.Fatalf("Key = %q", got)
	}
	if got := rowValue(rows, "Size"); got != "1.2K (1234 bytes)" {
		t.Fatalf("Size = %q, want human + exact bytes", got)
	}
	for key, want := range map[string]string{
		"Content-Type":     "text/plain",
		"ETag":             `"abc123"`,
		"Storage-Class":    "STANDARD_IA",
		"Version-ID":       "v1",
		"Cache-Control":    "max-age=60",
		"Content-Encoding": "gzip",
		"SSE":              "aws:kms",
		"SSE-KMS-Key":      "kms-key-1",
		"Checksum-SHA256":  "deadbeef",
	} {
		if got := rowValue(rows, key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	// Last-Modified renders in local time.
	if got := rowValue(rows, "Last-Modified"); got != mod.Local().Format(timeFormat) {
		t.Errorf("Last-Modified = %q, want local %q", got, mod.Local().Format(timeFormat))
	}
	// Unset fields are omitted, never rendered blank.
	for _, absent := range []string{"Content-Disposition", "Content-Language", "Expires",
		"Checksum-CRC32", "Replication", "Restore", "Object-Lock-Mode"} {
		if hasKey(rows, absent) {
			t.Errorf("empty field %s rendered", absent)
		}
	}
	// User metadata lands under its own heading, sorted.
	var heading, appIdx, ownerIdx int = -1, -1, -1
	for i, r := range rows {
		switch {
		case r.Key == "" && r.Value == "User metadata":
			heading = i
		case r.Key == "app":
			appIdx = i
		case r.Key == "owner":
			ownerIdx = i
		}
	}
	if heading < 0 || appIdx < heading || ownerIdx < appIdx {
		t.Fatalf("user metadata section wrong: heading=%d app=%d owner=%d", heading, appIdx, ownerIdx)
	}
	if got := rowValue(rows, "owner"); got != "lin" {
		t.Fatalf("user metadata owner = %q", got)
	}
}

func TestObjectRowsWithoutUserMetadataHasNoHeading(t *testing.T) {
	rows := objectRows("k", &s3.HeadObjectOutput{ContentLength: aws.Int64(1)})
	for _, r := range rows {
		if r.Value == "User metadata" {
			t.Fatal("User metadata heading rendered without any metadata")
		}
	}
}

func TestLocalRowsFileWithPermissionsAndOwner(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o640); err != nil {
		t.Fatal(err)
	}
	rows, err := localRows(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := rowValue(rows, "Name"); got != "f.txt" {
		t.Fatalf("Name = %q", got)
	}
	if got := rowValue(rows, "Path"); got != path {
		t.Fatalf("Path = %q, want the absolute path %q", got, path)
	}
	if got := rowValue(rows, "Type"); got != "file" {
		t.Fatalf("Type = %q", got)
	}
	if got := rowValue(rows, "Size"); got != "5 (5 bytes)" {
		t.Fatalf("Size = %q", got)
	}
	if got := rowValue(rows, "Permissions"); got != "-rw-r----- (0640)" {
		t.Fatalf("Permissions = %q, want rwx string + octal", got)
	}
	// This test suite runs on linux (the repo's platform), so the stat
	// extras must be present and the owner resolves to the current user.
	u, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	owner := rowValue(rows, "Owner")
	if owner == "" || !strings.Contains(owner, u.Uid) {
		t.Fatalf("Owner = %q, want the current uid %s", owner, u.Uid)
	}
	if u.Username != "" && !strings.Contains(owner, u.Username) {
		t.Fatalf("Owner = %q, want the resolved username %q", owner, u.Username)
	}
	for _, key := range []string{"Group", "Modified", "Accessed", "Changed"} {
		if !hasKey(rows, key) {
			t.Errorf("row %s missing", key)
		}
	}
}

func TestLocalRowsDirectoryCountsEntries(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 3; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%d", i)), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := localRows(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := rowValue(rows, "Type"); got != "directory" {
		t.Fatalf("Type = %q", got)
	}
	if got := rowValue(rows, "Size"); got != "3 entries" {
		t.Fatalf("Size = %q, want the entry count", got)
	}
}

func TestLocalRowsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	rows, err := localRows(link)
	if err != nil {
		t.Fatal(err)
	}
	if got := rowValue(rows, "Type"); got != "symlink → "+target {
		t.Fatalf("Type = %q, want the readlink target", got)
	}
	if got := rowValue(rows, "Target exists"); got != "yes" {
		t.Fatalf("Target exists = %q", got)
	}

	// Broken link: the target row flips, size/permissions describe the link.
	broken := filepath.Join(dir, "broken")
	if err := os.Symlink(filepath.Join(dir, "gone"), broken); err != nil {
		t.Fatal(err)
	}
	rows, err = localRows(broken)
	if err != nil {
		t.Fatal(err)
	}
	if got := rowValue(rows, "Target exists"); got != "no (broken link)" {
		t.Fatalf("Target exists = %q for a broken link", got)
	}
}

func TestProfileRows(t *testing.T) {
	rows := profileRows("oss", "https://oss-cn-shanghai.aliyuncs.com", "cn-shanghai",
		[]string{"/home/u/.aws/config", "/home/u/.aws/credentials"})
	if got := rowValue(rows, "Profile"); got != "oss" {
		t.Fatalf("Profile = %q", got)
	}
	if got := rowValue(rows, "Endpoint"); got != "https://oss-cn-shanghai.aliyuncs.com" {
		t.Fatalf("Endpoint = %q", got)
	}
	if got := rowValue(rows, "Region"); got != "cn-shanghai" {
		t.Fatalf("Region = %q", got)
	}
	files := 0
	for _, r := range rows {
		if r.Key == "Config file" {
			files++
		}
	}
	if files != 2 {
		t.Fatalf("Config file rows = %d, want 2", files)
	}
	// Empty endpoint/region are omitted.
	rows = profileRows("default", "", "", nil)
	if hasKey(rows, "Endpoint") || hasKey(rows, "Region") {
		t.Fatal("empty endpoint/region rendered")
	}
}

func TestStaleLoadedMsgDropped(t *testing.T) {
	m := NewModel()
	m.SetSize(90, 30)
	m.ShowProfile("first", "", "", nil)
	staleSeq := m.seq
	m.Hide()
	m.ShowProfile("second", "", "", nil)

	nm, _ := m.Update(LoadedMsg{Seq: staleSeq, Rows: []Row{{Key: "Profile", Value: "stale"}}})
	m = nm
	if rowValue(m.rows, "Profile") != "second" {
		t.Fatalf("stale LoadedMsg overwrote the rows: %v", m.rows)
	}
}

func TestHideDropsInFlightLoad(t *testing.T) {
	m := NewModel()
	m.SetSize(90, 30)
	cmd := m.ShowLocal(filepath.Join(t.TempDir(), "nope"))
	m.Hide()
	nm, _ := m.Update(cmd())
	m = nm
	if m.IsVisible() {
		t.Fatal("a late load re-opened the overlay")
	}
}

func TestViewAlignsRowsAndScrolls(t *testing.T) {
	m := NewModel()
	m.SetSize(120, 8) // page = 8-4-2-2 = tiny, forces scrolling
	m.open("title")
	m.loading = false
	for i := 0; i < 20; i++ {
		m.rows = append(m.rows, Row{Key: fmt.Sprintf("K%02d", i), Value: fmt.Sprintf("v%02d", i)})
	}
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "K00:") {
		t.Fatalf("first row missing:\n%s", out)
	}
	if !strings.Contains(out, "j/k scroll") {
		t.Fatalf("scroll footer missing on overflowing content:\n%s", out)
	}
	// Box width caps at min(80, 120-6) = 80.
	for i, l := range strings.Split(out, "\n") {
		if w := ansi.StringWidth(l); w > 80 {
			t.Fatalf("line %d is %d cells, want <= 80:\n%s", i, w, l)
		}
	}
	m.HandleKey("G")
	out = ansi.Strip(m.View())
	if !strings.Contains(out, "K19:") {
		t.Fatalf("G did not scroll to the last row:\n%s", out)
	}
	// Values align at one column: every rendered row's value starts at the
	// same offset (keys here share a width).
	m.HandleKey("g")
	out = ansi.Strip(m.View())
	first := -1
	for _, l := range strings.Split(out, "\n") {
		if idx := strings.Index(l, "v"); strings.Contains(l, "K0") && idx >= 0 {
			if first < 0 {
				first = idx
			} else if idx != first {
				t.Fatalf("row values misaligned: %d vs %d\n%s", idx, first, out)
			}
		}
	}
}
