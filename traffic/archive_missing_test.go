package traffic

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArchivePreservesDeletedBodyReferencesWithoutInventingData(t *testing.T) {
	root := t.TempDir()
	tr, err := Open(root, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()
	const host = "deleted-body.example"
	insertArchiveRecoveryExchange(t, tr, "deleted-body", host)
	present := tr.spill([]byte(strings.Repeat("present", 50_000)), "text/plain")
	deleted := tr.spill([]byte(strings.Repeat("deleted", 50_000)), "text/plain")
	if _, err := tr.DB().Exec(`INSERT INTO exchange_bodies(id,req_head,req_body,req_blob,resp_head,resp_body,resp_blob) VALUES(?,?,?,?,?,?,?)`, "deleted-body", "POST /", present.inline, present.hash, "HTTP 200", deleted.inline, deleted.hash); err != nil {
		t.Fatal(err)
	}
	path, err := tr.blobPath(deleted.hash)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "archive")
	count, missing, err := tr.ExportHostsWithMissing([]string{host}, dest)
	if err != nil || count != 1 || len(missing) != 1 || missing[0] != deleted.hash {
		t.Fatalf("count=%d missing=%v err=%v", count, missing, err)
	}
	raw, err := os.ReadFile(filepath.Join(dest, "traffic.json"))
	if err != nil {
		t.Fatal(err)
	}
	var snapshot ArchiveSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Blobs) != 1 || snapshot.Blobs[0] != present.hash || len(snapshot.MissingBlobs) != 1 || snapshot.MissingBlobs[0] != deleted.hash {
		t.Fatalf("incorrect present/missing blob manifest: %+v", snapshot)
	}
	if snapshot.Exchanges[0].RespBlob != deleted.hash || string(snapshot.Exchanges[0].RespBody) != string(deleted.inline) {
		t.Fatal("original missing-body reference or preview was lost")
	}
	if _, err := os.Stat(filepath.Join(dest, "blobs", deleted.hash+".bin")); !os.IsNotExist(err) {
		t.Fatal("deleted body must not be recreated or replaced")
	}
	otherRoot := t.TempDir()
	other, err := Open(otherRoot, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if imported, err := other.ImportArchive(dest); err != nil || imported != 1 {
		t.Fatalf("restore imported=%d err=%v", imported, err)
	}
	if _, err := other.blobPath(present.hash); err != nil {
		t.Fatal("available body was not restored", err)
	}
	if _, err := other.blobPath(deleted.hash); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted body must remain explicitly missing after restore: %v", err)
	}
}

func TestArchiveDoesNotIgnoreInvalidBlobReferences(t *testing.T) {
	tr, err := Open(t.TempDir(), "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()
	insertArchiveRecoveryExchange(t, tr, "bad-blob", "bad-blob.example")
	if _, err := tr.DB().Exec(`INSERT INTO exchange_bodies(id,req_head,resp_head,resp_blob) VALUES(?,?,?,?)`, "bad-blob", "GET /", "HTTP 200", "../invalid"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := tr.ExportHostsWithMissing([]string{"bad-blob.example"}, t.TempDir()); err == nil {
		t.Fatal("malformed references must still fail validation")
	}
}
