package service

import (
	"context"

	"github.com/teamopt/backend/internal/validator"
)

// RunService orchestrates team-formation runs and their validation.
// It is intentionally thin at this stage: run persistence lives in upcoming
// migrations/repositories (task 3.6). The ValidateVariant seam is provided now
// so that task 3.5's validator can be wired from day one.
type RunService struct {
	capSvc    *CapacityService
	validator *validator.Validator
}

// NewRunService constructs a RunService.
func NewRunService(capSvc *CapacityService) *RunService {
	return &RunService{
		capSvc:    capSvc,
		validator: validator.New(),
	}
}

// ValidateVariant validates a proposed variant against the authoritative snapshot.
// The caller provides the ValidateInput assembled from the run snapshot, requirements,
// candidates and the capacity matrix (built via CapacityService.GetCapacityMatrix).
// When run repositories are available, a future overload will load the snapshot here
// and then call this same validator.
func (s *RunService) ValidateVariant(ctx context.Context, input validator.ValidateInput) (validator.ValidationResult, error) {
	// Future: load run snapshot, requirements, candidates and constraints from repositories.
	// Future: build CapacityMatrix with s.capSvc.GetCapacityMatrix(ctx, employeeIDs, startWeek, endWeek).
	_ = ctx // reserved for repository calls above.
	return s.validator.Validate(input)
}
