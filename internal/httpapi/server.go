package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/hanfezh/restock-notification/internal/app"
)

func NewServer(service *app.Service, worker *app.Worker) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /products", createProduct(service))
	mux.HandleFunc("POST /products/{id}/registrations", registerRestock(service))
	mux.HandleFunc("POST /products/{id}/replenish", replenish(service))
	mux.HandleFunc("PUT /customers/{id}/mute-window", setMuteWindow(service))
	mux.HandleFunc("GET /notifications", listNotifications(service))
	mux.HandleFunc("POST /worker/process", processWorker(worker))
	return mux
}

func createProduct(service *app.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in app.CreateProductInput
		if err := decodeJSON(r, &in); err != nil {
			writeError(w, err)
			return
		}
		product, err := service.CreateProduct(r.Context(), in)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, product)
	}
}

func registerRestock(service *app.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		productID, err := parsePathID(r)
		if err != nil {
			writeError(w, err)
			return
		}
		var in app.RegisterRestockInput
		if err := decodeJSON(r, &in); err != nil {
			writeError(w, err)
			return
		}
		in.ProductID = productID
		registration, err := service.RegisterRestock(r.Context(), in)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, registration)
	}
}

func replenish(service *app.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		productID, err := parsePathID(r)
		if err != nil {
			writeError(w, err)
			return
		}
		var in struct {
			Quantity int `json:"quantity"`
		}
		if err := decodeJSON(r, &in); err != nil {
			writeError(w, err)
			return
		}
		result, err := service.Replenish(r.Context(), productID, in.Quantity)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func setMuteWindow(service *app.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		customerID, err := parsePathID(r)
		if err != nil {
			writeError(w, err)
			return
		}
		var in app.SetMuteWindowInput
		if err := decodeJSON(r, &in); err != nil {
			writeError(w, err)
			return
		}
		in.CustomerID = customerID
		if err := service.SetMuteWindow(r.Context(), in); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func listNotifications(service *app.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		notifications, err := service.ListNotifications(r.Context())
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, notifications)
	}
}

func processWorker(worker *app.Worker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Limit int `json:"limit"`
		}
		if err := decodeJSON(r, &in); err != nil {
			writeError(w, err)
			return
		}
		result, err := worker.ProcessBatch(r.Context(), in.Limit)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func parsePathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, app.ErrInvalidInput
	}
	return id, nil
}

func decodeJSON(r *http.Request, out any) error {
	if err := json.NewDecoder(r.Body).Decode(out); err != nil {
		return app.ErrInvalidInput
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, app.ErrInvalidInput):
		status = http.StatusBadRequest
	case errors.Is(err, app.ErrProductInStock):
		status = http.StatusConflict
	case errors.Is(err, app.ErrNotFound):
		status = http.StatusNotFound
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
