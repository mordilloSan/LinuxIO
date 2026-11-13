package handlers

import (
	"encoding/json/v2"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

var secretField = regexp.MustCompile(`(?i)password|passphrase|secret|private_?key`)

// A job record keeps its request for weeks: every Mutation request field that
// names a secret is tagged `secret:"true"`, so the record leaves it out.
func TestMutationRequestsKeepTheirSecretsOutOfTheRecord(t *testing.T) {
	for _, spec := range Routes {
		if spec.Kind != apischema.KindMutation {
			continue
		}
		request := reflect.New(spec.Request.GoType).Elem()
		secrets := fillSecrets(request)
		if len(secrets) == 0 {
			continue
		}
		raw, err := json.Marshal(request.Interface())
		if err != nil {
			t.Fatalf("%s: marshal request: %v", spec.Route, err)
		}
		if recorded := (bridgeipc.Route{Secrets: spec.Secrets}).RecordedRequest(raw); strings.Contains(string(recorded), "s3cret") {
			t.Errorf("%s: recorded request %s keeps %v; tag the field secret:\"true\"", spec.Route, recorded, secrets)
		}
	}
}

// fillSecrets sets every string field of v, a struct, whose JSON name names a
// secret, and returns those names.
func fillSecrets(v reflect.Value) []string {
	if v.Kind() != reflect.Struct {
		return nil
	}
	var names []string
	for i := range v.NumField() {
		field, value := v.Type().Field(i), v.Field(i)
		if !field.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "" {
			name = field.Name
		}
		switch {
		case value.Kind() == reflect.Struct:
			names = append(names, fillSecrets(value)...)
		case value.Kind() == reflect.String && secretField.MatchString(name):
			value.SetString("s3cret")
			names = append(names, name)
		}
	}
	return names
}
