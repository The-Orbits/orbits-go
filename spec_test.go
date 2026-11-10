package orbits

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/The-Orbits/orbits-go/internal/gen"
	"github.com/The-Orbits/orbits-go/internal/schema"
)

// operationMethods maps every operationId the SDK covers to the method that calls it. TestSpecCoverage fails
// when the spec has an operation that is neither here nor in gen.Skipped, so a new endpoint cannot ship without a
// decision. Method expressions make a renamed or deleted method a compile error.
var operationMethods = map[string]any{
	"getCurrentUser": (*MeService).Get,

	"listTeams":       (*TeamsService).List,
	"getTeam":         (*TeamsService).Get,
	"updateTeam":      (*TeamsService).Update,
	"getTeamOverview": (*TeamsService).GetOverview,

	"listNotifications":     (*TeamsService).ListNotifications,
	"markNotificationsRead": (*TeamsService).MarkNotificationsRead,

	"listPlans":    (*BillingService).ListPlans,
	"getBilling":   (*BillingService).Get,
	"listInvoices": (*BillingService).ListInvoices,
	"getInvoice":   (*BillingService).GetInvoice,

	"listTemplates": (*TemplatesService).List,

	"listProjects":             (*ProjectsService).List,
	"createProject":            (*ProjectsService).Create,
	"getProject":               (*ProjectsService).Get,
	"updateProject":            (*ProjectsService).Update,
	"deleteProject":            (*ProjectsService).Delete,
	"getProjectDeletionImpact": (*ProjectsService).GetDeletionImpact,
	"pauseProject":             (*ProjectsService).SetPaused,

	"listTeamDeployments":     (*DeploymentsService).ListTeam,
	"listProjectDeployments":  (*DeploymentsService).List,
	"createDeployment":        (*DeploymentsService).Create,
	"startSourceUpload":       (*DeploymentsService).StartUpload,
	"completeSourceUpload":    (*DeploymentsService).CompleteUpload,
	"cancelSourceUpload":      (*DeploymentsService).CancelUpload,
	"setProductionDeployment": (*DeploymentsService).SetProduction,
	"getDeployment":           (*DeploymentsService).Get,
	"listDeploymentLogs":      (*DeploymentsService).ListLogs,
	"streamDeploymentLogs":    (*DeploymentsService).StreamLogs,
	"getDeploymentScreenshot": (*DeploymentsService).GetScreenshot,
	"redeployDeployment":      (*DeploymentsService).Redeploy,
	"promoteDeployment":       (*DeploymentsService).Promote,
	"rollbackDeployment":      (*DeploymentsService).Rollback,
	"cancelDeployment":        (*DeploymentsService).Cancel,

	"listRuntimeLogs":   (*ObservabilityService).ListLogs,
	"getRuntimeLog":     (*ObservabilityService).GetLog,
	"getObservability":  (*ObservabilityService).Get,
	"getAnalytics":      (*ObservabilityService).GetAnalytics,
	"setAnalytics":      (*ObservabilityService).SetAnalytics,
	"getAnalyticsEvent": (*ObservabilityService).GetAnalyticsEvent,
	"getSpeedInsights":  (*ObservabilityService).GetSpeedInsights,
	"setSpeedInsights":  (*ObservabilityService).SetSpeedInsights,

	"listFlags":          (*FlagsService).List,
	"createFlag":         (*FlagsService).Create,
	"getFlagKeys":        (*FlagsService).GetKeys,
	"getFlag":            (*FlagsService).Get,
	"updateFlag":         (*FlagsService).Update,
	"deleteFlag":         (*FlagsService).Delete,
	"setFlagEnvironment": (*FlagsService).SetEnvironment,
	"addFlagRule":        (*FlagsService).AddRule,
	"updateFlagRule":     (*FlagsService).UpdateRule,
	"removeFlagRule":     (*FlagsService).RemoveRule,

	"getWorkflows":      (*WorkflowsService).List,
	"runWorkflow":       (*WorkflowsService).Run,
	"getWorkflowRun":    (*WorkflowsService).GetRun,
	"cancelWorkflowRun": (*WorkflowsService).CancelRun,
	"retryWorkflowRun":  (*WorkflowsService).RetryRun,

	"listSandboxes": (*SandboxesService).List,
	"createSandbox": (*SandboxesService).Create,
	"getSandbox":    (*SandboxesService).Get,
	"deleteSandbox": (*SandboxesService).Delete,
	"stopSandbox":   (*SandboxesService).Stop,
	"startSandbox":  (*SandboxesService).Start,

	"createSandboxTerminalTicket": (*SandboxesService).TerminalTicket,

	"listAlerts":       (*AlertsService).List,
	"createAlert":      (*AlertsService).Create,
	"previewAlert":     (*AlertsService).Preview,
	"updateAlert":      (*AlertsService).Update,
	"deleteAlert":      (*AlertsService).Delete,
	"listAlertEvents":  (*AlertsService).ListEvents,
	"getAlertChannels": (*AlertsService).GetChannels,

	"setAlertWebhook":    (*AlertsService).SetWebhook,
	"deleteAlertWebhook": (*AlertsService).DeleteWebhook,
	"testAlertWebhook":   (*AlertsService).TestWebhook,

	"listEnvVars":   (*EnvService).List,
	"createEnvVars": (*EnvService).Create,
	"updateEnvVar":  (*EnvService).Update,
	"deleteEnvVar":  (*EnvService).Delete,
	"restoreEnvVar": (*EnvService).Restore,
	"revealEnvVar":  (*EnvService).Reveal,
	"pullEnvVars":   (*EnvService).Pull,

	"listDomains":         (*DomainsService).List,
	"addDomain":           (*DomainsService).Add,
	"previewDomain":       (*DomainsService).Preview,
	"getDomain":           (*DomainsService).Get,
	"updateDomain":        (*DomainsService).Update,
	"removeDomain":        (*DomainsService).Remove,
	"checkDomain":         (*DomainsService).Check,
	"listDnsRecords":      (*DomainsService).ListRecords,
	"addDnsRecord":        (*DomainsService).AddRecord,
	"replaceDnsRecord":    (*DomainsService).ReplaceRecord,
	"deleteDnsRecord":     (*DomainsService).DeleteRecord,
	"importZoneFile":      (*DomainsService).ImportZone,
	"getEmailSetup":       (*DomainsService).GetEmailSetup,
	"setEmailProvider":    (*DomainsService).SetEmailProvider,
	"listBranchAddresses": (*DomainsService).ListBranchAddresses,

	"listIntegrations": (*IntegrationsService).List,
	"getUsage":         (*UsageService).Get,
	"listActivity":     (*ActivityService).List,
	"getOperation":     (*OperationsService).Get,
}

func TestSpecCoverage(t *testing.T) {
	spec := testSpec(t)
	inSpec := map[string]bool{}
	for _, op := range spec.Operations() {
		inSpec[op.ID] = true
		_, skipped := gen.Skipped[op.ID]
		_, mapped := operationMethods[op.ID]
		switch {
		case skipped && mapped:
			t.Errorf("%s has a method and is in gen.Skipped: remove it from one", op.ID)
		case !skipped && !mapped:
			t.Errorf("spec operation %s (%s %s) has no method: add one and list it in operationMethods, or skip it in internal/gen/config.go", op.ID, op.Method, op.Path)
		}
	}
	for id := range gen.Skipped {
		if !inSpec[id] {
			t.Errorf("gen.Skipped lists %s, which is not in the spec", id)
		}
	}
	for id := range operationMethods {
		if !inSpec[id] {
			t.Errorf("operationMethods lists %s, which is not in the spec", id)
		}
	}
}

// TestSpecRoutes calls every mapped method against a server that answers with an example built from the spec's
// response schema, and checks the method sent the spec's method and path (with an Idempotency-Key on a POST),
// and that what it returned encodes back to a document the schema accepts.
func TestSpecRoutes(t *testing.T) {
	spec := testSpec(t)
	ops := map[string]schema.Operation{}
	for _, op := range spec.Operations() {
		ops[op.ID] = op
	}
	var mu sync.Mutex
	var got []*http.Request
	var current schema.Operation
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = append(got, r)
		op := current
		mu.Unlock()
		if strings.HasSuffix(r.URL.Path, "/logs/stream") {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "event: done\ndata: {\"state\":\"ready\",\"failure\":null,\"duration_seconds\":3}\n\n")
			return
		}
		for _, resp := range op.Responses {
			if !strings.HasPrefix(resp.Status, "2") {
				continue
			}
			code := 200
			fmt.Sscan(resp.Status, &code)
			switch {
			case resp.ContentType == "application/json":
				body, _ := json.Marshal(spec.Example(resp.Schema, 0))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(code)
				_, _ = w.Write(body)
			case resp.ContentType != "":
				w.Header().Set("Content-Type", resp.ContentType)
				w.WriteHeader(code)
				_, _ = w.Write([]byte("binary"))
			default:
				w.WriteHeader(code)
			}
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c := newTestClient(t, srv.URL)

	for id, fn := range operationMethods {
		op, ok := ops[id]
		if !ok {
			continue
		}
		mu.Lock()
		got, current = nil, op
		mu.Unlock()
		results := callWithPlaceholders(t, c, fn)
		mu.Lock()
		reqs := got
		mu.Unlock()
		if len(reqs) != 1 {
			t.Errorf("%s: made %d requests, want 1", id, len(reqs))
			continue
		}
		r := reqs[0]
		// QuoteMeta escapes the braces of {param}, hence the escaped pattern.
		want := regexp.MustCompile("^/v1" + regexp.MustCompile(`\\\{[^}]+\\\}`).ReplaceAllString(regexp.QuoteMeta(op.Path), `[^/]+`) + "$")
		if r.Method != op.Method || !want.MatchString(r.URL.Path) {
			t.Errorf("%s: sent %s %s, want %s %s", id, r.Method, r.URL.Path, op.Method, op.Path)
		}
		if r.Method == http.MethodPost && r.Header.Get("Idempotency-Key") == "" {
			t.Errorf("%s: POST sent without an Idempotency-Key", id)
		}
		if r.Header.Get("Authorization") != "Bearer "+testToken {
			t.Errorf("%s: Authorization %q", id, r.Header.Get("Authorization"))
		}
		for _, p := range op.Params {
			if schema.Str(p, "in") == "query" && schema.Bool(p, "required") {
				// Placeholder params are zero values, so a required query parameter may be missing; that is
				// the caller's to set. Only check the name is one the spec knows.
				continue
			}
		}
		for q := range r.URL.Query() {
			if op.Param("query", q) == nil {
				t.Errorf("%s: sent query parameter %q, which the spec does not have", id, q)
			}
		}
		if err := resultError(results); err != nil {
			t.Errorf("%s: %v", id, err)
			continue
		}
		if success := op.Success(); success != nil && len(results) == 2 {
			data, err := json.Marshal(results[0].Interface())
			if err != nil {
				t.Errorf("%s: encoding the result: %v", id, err)
				continue
			}
			if errs := spec.Validate(success.Schema, decodeAny(t, string(data))); len(errs) > 0 {
				t.Errorf("%s: the decoded answer does not match the spec: %s", id, strings.Join(errs, "; "))
			}
		}
	}
}

func resultError(results []reflect.Value) error {
	if len(results) == 0 {
		return nil
	}
	last := results[len(results)-1]
	if last.Type() == reflect.TypeOf((*error)(nil)).Elem() && !last.IsNil() {
		return last.Interface().(error)
	}
	return nil
}

// callWithPlaceholders calls a method expression with its service from c and placeholder arguments, and drains
// an iterator if it returns one. It returns the method's results.
func callWithPlaceholders(t *testing.T, c *Client, fn any) []reflect.Value {
	t.Helper()
	v := reflect.ValueOf(fn)
	ft := v.Type()
	args := []reflect.Value{serviceFor(t, c, ft.In(0)), reflect.ValueOf(context.Background())}
	n := ft.NumIn()
	if ft.IsVariadic() {
		n-- // ...RequestOption: Call fills it in
	}
	for i := 2; i < n; i++ {
		switch in := ft.In(i); in.Kind() {
		case reflect.String:
			args = append(args, reflect.ValueOf("ph_"+string(rune('a'+i))).Convert(in))
		case reflect.Bool:
			args = append(args, reflect.ValueOf(true))
		default:
			args = append(args, reflect.Zero(in))
		}
	}
	out := v.Call(args)
	if len(out) == 1 && out[0].Kind() == reflect.Func { // iter.Seq2
		var iterErr error
		yield := reflect.MakeFunc(out[0].Type().In(0), func(a []reflect.Value) []reflect.Value {
			if !a[1].IsNil() {
				iterErr = a[1].Interface().(error)
			}
			return []reflect.Value{reflect.ValueOf(true)}
		})
		out[0].Call([]reflect.Value{yield})
		if iterErr != nil {
			return []reflect.Value{reflect.ValueOf(&iterErr).Elem()}
		}
		return nil
	}
	return out
}

func serviceFor(t *testing.T, c *Client, typ reflect.Type) reflect.Value {
	t.Helper()
	cv := reflect.ValueOf(c).Elem()
	for i := 0; i < cv.NumField(); i++ {
		if f := cv.Field(i); f.Type() == typ && f.CanInterface() {
			return f
		}
	}
	t.Fatalf("Client has no field of type %s", typ)
	return reflect.Value{}
}

// TestTypesMatchSpec decodes examples of every request and response schema the SDK uses into its Go type,
// refusing unknown fields (so every property in the spec has a field), encodes them back, and checks nothing was
// lost or renamed. Examples are built with every optional property present, over several oneOf variants and with
// required nullable properties both set and null.
func TestTypesMatchSpec(t *testing.T) {
	spec := testSpec(t)
	ops := map[string]schema.Operation{}
	for _, op := range spec.Operations() {
		ops[op.ID] = op
	}
	for id, fn := range operationMethods {
		op := ops[id]
		ft := reflect.TypeOf(fn)
		if success := op.Success(); success != nil && ft.NumOut() == 2 && ft.Out(0).Kind() == reflect.Pointer {
			checkRoundTrip(t, spec, id+" response", success.Schema, ft.Out(0).Elem())
		}
		if op.Body != nil {
			if bt := requestType(ft); bt != nil {
				checkRoundTrip(t, spec, id+" request", op.Body, bt)
			} else if !bodyBuiltByMethod[id] {
				t.Errorf("%s has a request body, but %s takes no request struct", id, ft)
			}
		}
	}
}

// bodyBuiltByMethod lists operations whose method builds a small body from plain arguments.
var bodyBuiltByMethod = map[string]bool{
	"pauseProject": true, "setAnalytics": true, "setSpeedInsights": true, "pullEnvVars": true,
}

// requestType is the struct parameter of a method, if it takes one by value.
func requestType(ft reflect.Type) reflect.Type {
	for i := 2; i < ft.NumIn(); i++ {
		if in := ft.In(i); in.Kind() == reflect.Struct && !strings.HasSuffix(in.Name(), "Params") {
			return in
		}
	}
	return nil
}

func checkRoundTrip(t *testing.T, spec *schema.Spec, label string, sch any, typ reflect.Type) {
	t.Helper()
	for variant := 0; variant < 4; variant++ {
		example := spec.Example(sch, variant)
		data, err := json.Marshal(example)
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		v := reflect.New(typ)
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err := dec.Decode(v.Interface()); err != nil {
			t.Errorf("%s (variant %d): decoding into %s: %v\n%s", label, variant, typ, err, data)
			continue
		}
		back, err := json.Marshal(v.Interface())
		if err != nil {
			t.Errorf("%s: encoding %s: %v", label, typ, err)
			continue
		}
		// null and absent are the same to a response type: a property only some oneOf variants have is optional
		// in Go, and leaves out its null.
		if a, b := dropNulls(decodeAny(t, string(data))), dropNulls(decodeAny(t, string(back))); !reflect.DeepEqual(a, b) {
			t.Errorf("%s (variant %d): %s does not round-trip\n spec: %s\n   go: %s", label, variant, typ, data, back)
		}
	}
}

// TestGeneratedFilesUpToDate fails when types.go or params.go differ from what the generator makes of the
// vendored spec: run go run ./scripts/gentypes.
func TestGeneratedFilesUpToDate(t *testing.T) {
	types, params, err := gen.Generate(testSpec(t))
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string][]byte{"types.go": types, "params.go": params} {
		have, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(have, want) {
			t.Errorf("%s is out of date with testdata/openapi.yaml: run go run ./scripts/gentypes", name)
		}
	}
}

// dropNulls removes object properties whose value is null.
func dropNulls(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, e := range x {
			if e != nil {
				out[k] = dropNulls(e)
			}
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = dropNulls(e)
		}
		return out
	}
	return v
}
