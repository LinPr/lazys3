package objectlist

import (
	"context"
	"fmt"
	"log"
	"strings"

	s3store "github.com/LinPr/lazys3/internal/storage/s3"
	"github.com/LinPr/lazys3/internal/storage/uri"
	tea "github.com/charmbracelet/bubbletea/v2"
)

type Object struct {
	name string
}

type Option struct {
	S3Uri       string
	Profile     string
	Region      string
	PathStyle   bool
	EndpointUrl string
}

func (b Object) Title() string       { return b.name }
func (b Object) Description() string { return "null" }
func (b Object) FilterValue() string { return b.name }

func (i Object) GetPreviewContent() string {
	// 通过网络获取到数据
	var content strings.Builder
	content.WriteString("• Versioning: Enabled\n")
	content.WriteString("• Encryption: AES-256\n")
	content.WriteString("• Public Access: Blocked\n")

	return content.String()
}

type FetchObjectListResultMsg struct {
	Objects []Object
	Err     error
}

func FetchObjectListCmd(o Option) tea.Cmd {
	log.Println("111111111111111111")
	return func() tea.Msg {
		log.Println("22222222222222222")
		opt := s3store.S3Option{
			UsePathStyle: o.PathStyle,
			Region:       o.Region,
			Profile:      o.Profile,
			// Endpoint:     o.EndpointUrl,
			// NoVerifySSL: o.NoVerifySSL,
			// DryRun:      o.DryRun,
		}

		cli, err := s3store.NewS3Client(context.TODO(), opt)
		if err != nil {
			return FetchObjectListResultMsg{Err: err}
		}

		if o.S3Uri == "" {
			return FetchObjectListResultMsg{Err: fmt.Errorf("s3uri is empty")}
		}

		parsedUri, err := uri.ParseS3Uri(o.S3Uri)
		if err != nil {
			return FetchObjectListResultMsg{Err: err}
		}
		if parsedUri.GetBucket() == "" {
			return FetchObjectListResultMsg{Err: fmt.Errorf("bucket is empty")}
		}
		objects, err := listObjects(cli, parsedUri.GetBucket(), parsedUri.GetKey())
		if err != nil {
			return FetchObjectListResultMsg{Err: err}
		}
		if len(objects) == 0 {
			return FetchObjectListResultMsg{Err: fmt.Errorf("no object found")}
		}
		log.Printf("-----objects: %#v\n", objects)
		return FetchObjectListResultMsg{Objects: objects, Err: nil}
	}
}

func listObjects(cli *s3store.S3Store, bucket, key string) ([]Object, error) {
	prefixes, objs, err := cli.ListObjectsWithPagination(context.TODO(), bucket, key)
	if err != nil {
		return nil, err
	}

	objectList := make([]Object, 0, len(prefixes)+len(objs))
	for _, prefix := range prefixes {
		// log.Println("-------------prefix:", *prefix.Prefix)
		objectList = append(objectList, Object{name: *prefix.Prefix})
	}
	for _, obj := range objs {
		// log.Println("-------------object:", *obj.Key)
		objectList = append(objectList, Object{name: *obj.Key})
	}
	return objectList, nil
}
