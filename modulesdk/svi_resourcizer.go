package modulesdk

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	modulert "github.com/StellaxiaLab/terra-sdk/modulert"
)

// SVIResourcizer is the module side of the resourcizer seam. Package
// modulesdk/resourcize implements it; a module rarely writes one by hand.
type SVIResourcizer interface {
	// Resourcizers lists the types this module offers.
	Resourcizers(ctx context.Context) modulert.ResourcizerList
	// ApplyInstances makes the module's instances the given set and answers
	// every instance's status. The same set twice changes nothing.
	ApplyInstances(ctx context.Context, set modulert.InstanceSet) modulert.InstanceSetResult
	// ValidateInstance checks one instance without creating it: 200 and no
	// errors, 422 with field errors, 404 for a type this module lacks.
	ValidateInstance(ctx context.Context, spec modulert.InstanceSpec) (int, modulert.InstanceValidateResponse)
}

// maxInstanceBody bounds an instance set. A few hundred instances with their
// configs fit many times over.
const maxInstanceBody = 4 << 20

func sviResourcizersHandler(resourcizer SVIResourcizer) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, resourcizer.Resourcizers(r.Context()))
	})
}

func sviInstancesHandler(resourcizer SVIResourcizer) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var set modulert.InstanceSet
		if err := json.NewDecoder(io.LimitReader(r.Body, maxInstanceBody)).Decode(&set); err != nil {
			// A set that does not decode — truncated included — changes
			// nothing. Applying a prefix of it would withdraw every instance
			// that fell off the end.
			http.Error(w, "invalid instance set", http.StatusBadRequest)
			return
		}
		writeJSON(w, resourcizer.ApplyInstances(r.Context(), set))
	})
}

func sviInstanceValidateHandler(resourcizer SVIResourcizer) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var spec modulert.InstanceSpec
		if err := json.NewDecoder(io.LimitReader(r.Body, maxInstanceBody)).Decode(&spec); err != nil {
			http.Error(w, "invalid instance", http.StatusBadRequest)
			return
		}
		status, response := resourcizer.ValidateInstance(r.Context(), spec)
		writeJSONStatus(w, status, response)
	})
}
