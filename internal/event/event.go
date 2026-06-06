package event

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
)

type Writer struct {
	Out     io.Writer
	Enabled bool
}

func (w Writer) Emit(kind string, fields map[string]interface{}) {
	if !w.Enabled || w.Out == nil {
		return
	}
	if fields == nil {
		fields = map[string]interface{}{}
	}
	fields["event"] = kind
	fields["time"] = time.Now().Format(time.RFC3339)
	b, err := json.Marshal(fields)
	if err != nil {
		fmt.Fprintf(w.Out, "{\"event\":\"json_error\",\"error\":%q}\n", err.Error())
		return
	}
	fmt.Fprintln(w.Out, string(b))
}
