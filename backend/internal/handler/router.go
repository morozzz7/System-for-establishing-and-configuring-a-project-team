package handler

import (
	"github.com/go-chi/chi/v5"

	"github.com/teamopt/backend/internal/service"
)

// NewRouter creates a chi.Router with all CRUD routes + capacity routes registered.
func NewRouter(
	empSvc *service.EmployeeService,
	prjSvc *service.ProjectService,
	asgSvc *service.AssignmentService,
	absSvc *service.AbsenceService,
	capSvc *service.CapacityService,
) chi.Router {
	r := chi.NewRouter()

	r.Route("/employees", func(r chi.Router) {
		h := &EmployeeHandler{Svc: empSvc}
		h.Register(r)
		if capSvc != nil {
			ch := &CapacityHandler{Svc: capSvc}
			ch.Register(r)
		}
	})

	r.Route("/projects", func(r chi.Router) {
		h := &ProjectHandler{Svc: prjSvc}
		h.Register(r)
		// Assignment routes are nested under /projects per the contract.
		ah := &AssignmentHandler{Svc: asgSvc}
		ah.Register(r)
	})

	r.Route("/absences", func(r chi.Router) {
		h := &AbsenceHandler{Svc: absSvc}
		h.Register(r)
	})

	if capSvc != nil {
		r.Get("/capacity/matrix", (&CapacityMatrixHandler{Svc: capSvc}).GetMatrix)
	}

	return r
}
