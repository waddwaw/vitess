/*
Copyright 2021 The Vitess Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"strings"
	"text/template"
)

const tmplStr = `// Code generated by {{ .ClientName }}-generator. DO NOT EDIT.

/*
Copyright 2021 The Vitess Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package {{ .PackageName }}

import (
	"context"

	"google.golang.org/grpc"
	{{ if not .Local -}}
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	{{- end }}
	{{ if .NeedsGRPCShim -}}
	"vitess.io/vitess/go/vt/vtctl/internal/grpcshim"
	{{- end }}

	{{ range .Imports -}}
	{{ if ne .Alias "" }}{{ .Alias }} {{ end }}"{{ .Path }}"
	{{ end -}}
)
{{ range .Methods }}
{{ if and $.Local .IsStreaming -}}
type {{ streamAdapterName .Name }} struct {
	*grpcshim.BidiStream
	ch chan {{ .StreamMessage.Type }}
}

func (stream *{{ streamAdapterName .Name }}) Recv() ({{ .StreamMessage.Type }}, error) {
	select {
	case <-stream.Context().Done():
		return nil, stream.Context().Err()
	case <-stream.Closed():
		// Stream has been closed for future sends. If there are messages that
		// have already been sent, receive them until there are no more. After
		// all sent messages have been received, Recv will return the CloseErr.
		select {
		case msg := <-stream.ch:
			return msg, nil
		default:
			return nil, stream.CloseErr()
		}
	case err := <-stream.ErrCh:
		return nil, err
	case msg := <-stream.ch:
		return msg, nil
	}
}

func (stream *{{ streamAdapterName .Name }}) Send(msg {{ .StreamMessage.Type }}) error {
	select {
	case <-stream.Context().Done():
		return stream.Context().Err()
	case <-stream.Closed():
		return grpcshim.ErrStreamClosed
	case stream.ch <- msg:
		return nil
	}
}
{{ end -}}
// {{ .Name }} is part of the vtctlservicepb.VtctldClient interface.
func (client *{{ $.Type }}) {{ .Name }}(ctx context.Context, {{ .Param.Name }} {{ .Param.Type }}, opts ...grpc.CallOption) ({{ .Result.Type }}, error) {
	{{ if not $.Local -}}
	if client.c == nil {
		return nil, status.Error(codes.Unavailable, connClosedMsg)
	}

	return client.c.{{ .Name }}(ctx, in, opts...)
	{{- else -}}
	{{- if .IsStreaming -}}
	stream := &{{ streamAdapterName .Name }}{
		BidiStream: grpcshim.NewBidiStream(ctx),
		ch:         make(chan {{ .StreamMessage.Type }}, 1),
	}
	go func() {
		err := client.s.{{ .Name }}(in, stream)
		stream.CloseWithError(err)
	}()

	return stream, nil
	{{- else -}}
	return client.s.{{ .Name }}(ctx, in)
	{{- end -}}
	{{- end }}
}
{{ end }}`

var tmpl = template.Must(template.New("vtctldclient-generator").Funcs(map[string]interface{}{
	"streamAdapterName": func(s string) string {
		if len(s) == 0 {
			return s
		}

		head := s[:1]
		tail := s[1:]
		return strings.ToLower(head) + tail + "StreamAdapter"
	},
}).Parse(tmplStr))
