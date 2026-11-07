# Task 2.4 Implementation Summary

## Overview
Implemented the weekly capacity and availability service for the team optimization system. This service computes employee capacity by accounting for norm hours, existing assignments, absences, and reserve ratios.

## Implemented Components

### 1. Domain Types (`backend/internal/domain/`)

#### `capacity.go`
- `WeeklyCapacity`: Represents capacity breakdown for an employee in a specific week
  - `AvailableMinutes`: Minutes available for new assignments
  - `AssignedMinutes`: Minutes already assigned to projects
  - `AbsentMinutes`: Minutes lost to absences
  - `NormMinutes`: Weekly norm in minutes
  - `UtilizationRatio`: (Assigned + Absent) / Norm
- `CapacityMatrix`: Collection of WeeklyCapacity records for an employee across multiple weeks

#### `week.go`
- `ParseWeek(s string)`: Parse ISO week format "2026-W01"
- `WeekRange(start, end string)`: Generate all weeks in a range
- `WeekMonday(year, week int)`: Get Monday date of an ISO week
- `WorkingDaysInWeek()`: Returns 5 (simplified, no holidays yet)
- `WeekContainsDate()`: Check if a date falls within a week
- `WorkingDaysInOverlap()`: Count Mon-Fri days between two dates

### 2. Repository Layer (`backend/internal/repository/capacity.go`)

#### Interface
```go
type CapacityRepository interface {
    GetWeeklyCapacity(ctx, employeeID, startWeek, endWeek)
    GetCapacityMatrix(ctx, employeeIDs, startWeek, endWeek)
    FindCandidatesByCompetency(ctx, competencyID, minLevel)
}
```

#### PostgreSQL Implementation (`PgCapacityRepo`)
- Batch queries to avoid N+1 problems
- Computes capacity using formula: `available = effective_limit - assigned - absent`
- `effective_limit = min(norm × (1-reserve), norm × max_util)`
- Counts actual working days in absence overlaps (Mon-Fri)
- Uses `hours_minutes` from DB (integer) for precision

### 3. Service Layer (`backend/internal/service/capacity.go`)

#### `CapacityService`
Three main methods:

1. **`CheckAvailability(employeeID, week, requestedMinutes)`**
   - Enforces G1 (absence) and G2 (overload) constraints
   - Returns `*domain.Problem` on violation with specific reason codes
   - G1: Rejects if employee has any absence in the week
   - G2: Rejects if `assigned + absent + requested > effective_limit`

2. **`GetAvailableHours(employeeID, weeks)`**
   - Returns capacity breakdown for specific weeks
   - Used by frontend and optimizer

3. **`FindAvailableEmployees(weeks, minMinutes, competencyID, minLevel)`**
   - Finds employees with required competency level
   - Filters by availability (≥ minMinutes in ALL specified weeks)
   - Returns list of available employee IDs

### 4. Handler Layer (`backend/internal/handler/handler.go`)

#### New Endpoints
- `GET /employees/{id}/capacity?start_week=...&end_week=...`
  - Returns capacity matrix for a single employee
- `GET /capacity/matrix?employee_ids=...&start_week=...&end_week=...`
  - Returns capacity for multiple employees (batch query)

### 5. Refactored Assignment Service (`backend/internal/service/service.go`)

- Added `CapacitySvc` field to `AssignmentService`
- `CreateBatch` now uses `CapacitySvc.CheckAvailability` for G1/G2 checks
- Maintains backward compatibility with legacy inline checks
- Better error messages with specific constraint violation details

### 6. Tests

#### Property Tests (`capacity_property_test.go`)
4 property-based tests using `testing/quick`:
1. **G1NoAbsenceConflict**: If employee is absent, CheckAvailability must fail
2. **G2NoOverload**: If total load > effective limit, must fail
3. **AvailableNonNegative**: Available minutes always ≥ 0
4. **CapacityMonotonic**: Adding assignments reduces available capacity

All tests run with 200-500 random iterations.

#### Integration Tests (`capacity_integration_test.go`)
7 integration tests with testcontainers-go:
1. `TestCapInt_BasicCapacityCalculation`: Verify capacity math
2. `TestCapInt_G1_AbsenceRejection`: Absence blocks assignment
3. `TestCapInt_G2_OverloadRejection`: Overload blocks assignment
4. `TestCapInt_AbsenceReducesCapacity`: Partial week absence
5. `TestCapInt_FindAvailableEmployees`: Competency + availability filter
6. `TestCapInt_CapacityMatrix`: Batch query for multiple employees
7. `TestCapInt_EdgeCase_ZeroAvailableAfterFullAssignment`: Boundary condition

### 7. Router Updates (`backend/internal/handler/router.go`)
- Added capacity routes under `/employees/{id}/capacity` and `/capacity/matrix`
- Wired `CapacityService` through dependency injection

### 8. Main Application (`backend/cmd/api/main.go`)
- Initialize `PgCapacityRepo` and `CapacityService`
- Pass to router and assignment service

## Key Design Decisions

### 1. Minutes vs Hours
- All capacity calculations use **minutes** internally
- DB stores `hours_minutes` as integer (no floating point errors)
- API accepts/returns minutes for precision

### 2. Effective Limit Calculation
```
effective_limit = min(norm × (1-reserve), norm × max_util)
available = max(0, effective_limit - assigned - absent)
```
- Reserve ratio reduces assignable capacity (buffer for unplanned work)
- Max utilization is absolute ceiling
- Both constraints enforced simultaneously

### 3. Absence Calculation
- Counts actual working days (Mon-Fri) in absence overlap
- Example: 3-day absence (Mon-Wed) = 3 × hours_per_day × 60 minutes
- Handles partial week absences correctly

### 4. Batch Optimization
- `GetCapacityMatrix` uses batch SQL queries
- Fetches all profiles, assignments, and absences in 3 queries
- Avoids N+1 problem for large employee lists

### 5. Backward Compatibility
- Assignment service keeps legacy G1/G2 checks as fallback
- New code path uses `CapacityService` when available
- Existing tests continue to pass without modification

## Test Results

```
✓ domain tests: PASS
✓ handler integration tests: PASS (6 tests)
✓ migration tests: PASS (all constraints verified)
✓ service unit tests: PASS (17 tests)
✓ capacity integration tests: PASS (7 tests)
✓ property tests: PASS (4 properties × 200-500 iterations)
```

## Performance Characteristics

- **Single employee capacity**: O(1) queries (3 SQL queries)
- **Batch capacity matrix**: O(1) queries regardless of employee count
- **FindAvailableEmployees**: O(1) queries with competency index
- **Memory**: Linear in number of weeks × employees

## Future Enhancements

1. **Russian Production Calendar**: Replace `WorkingDaysInWeek() = 5` with actual calendar
2. **Caching**: Cache capacity for hot paths (optimizer calls)
3. **Timezone Support**: Handle employees in different timezones
4. **Partial Day Absences**: Support hour-level absence granularity
5. **Capacity History**: Track capacity changes over time for trend analysis

## Metrics Impact

Per `docs/METRICS.md`:
- **Hard-violation rate = 0**: G1/G2 enforced via property tests
- **Determinism = 100%**: Capacity calculation is pure math, no LLM/randomness
- **Solver-validator agreement**: Go validator independently verifies all constraints

## Files Changed/Created

### Created
- `backend/internal/domain/capacity.go` (46 lines)
- `backend/internal/domain/week.go` (103 lines)
- `backend/internal/repository/capacity.go` (280 lines)
- `backend/internal/service/capacity.go` (220 lines)
- `backend/internal/service/capacity_property_test.go` (347 lines)
- `backend/internal/service/capacity_integration_test.go` (450 lines)

### Modified
- `backend/internal/service/service.go` (refactored AssignmentService)
- `backend/internal/handler/handler.go` (added capacity endpoints)
- `backend/internal/handler/router.go` (wired capacity routes)
- `backend/cmd/api/main.go` (initialized CapacityService)

Total: ~1,450 lines of production code + ~800 lines of tests
