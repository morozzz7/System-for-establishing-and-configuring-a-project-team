"""Router-level tests for POST /v1/optimize (schema-valid responses, header echo, errors,
deficit variants, variant-set labels and contract conformance)."""

from __future__ import annotations

import itertools
import pathlib
import re

import pytest
import pytest_asyncio
import yaml
from httpx import ASGITransport, AsyncClient

from app.main import app
from app.optimize.schemas import VALID_REASON_CODES, OptimizeResponse
from tests.optimize_helpers import REPO_ROOT, build_request_from_scenario, make_request

CONTRACT_PATH = REPO_ROOT / "contracts" / "ml-openapi.yaml"


@pytest_asyncio.fixture()
async def client():
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as c:
        yield c


# ---------------------------------------------------------------------------
# contract conformance
# ---------------------------------------------------------------------------

def _contract_validator():
    """JSON-Schema validator for ``OptimizeResponse`` from ``contracts/ml-openapi.yaml``."""
    jsonschema = pytest.importorskip("jsonschema", reason="optional contract check dependency")
    spec = yaml.safe_load(pathlib.Path(CONTRACT_PATH).read_text())
    root = {"$ref": "#/components/schemas/OptimizeResponse", "components": spec["components"]}
    validator_cls = getattr(jsonschema, "Draft7Validator", None) or jsonschema.validator_for(root)
    return validator_cls(root)


def _assert_contract_valid(payload: dict) -> None:
    validator = _contract_validator()
    errors = sorted(validator.iter_errors(payload), key=lambda e: list(e.path))
    assert not errors, "; ".join(f"{list(e.path)}: {e.message}" for e in errors)


def test_contract_validator_rejects_malformed_payload() -> None:
    """Guard against a vacuous conformance check."""
    validator = _contract_validator()
    bad = {
        "request_id": "req_router000001",
        "run_id": "run_test",
        "solver": {"name": "ortools-cp-sat", "version": "1", "status": "OPTIMAL",
                   "wall_time_ms": 1, "num_workers": 1, "seed": 1, "deterministic": True},
        "variants": [],
        "deterministic_fingerprint": "sha256:" + "0" * 64,
        "diagnostics": {"model_version": "opt-v2", "weeks": 1, "requirement_count": 1,
                        "candidate_count": 1, "variables": 1, "constraints": 1,
                        "time_limit_ms": 1000},
        "reason_codes": [],
        "unexpected_field": True,
    }
    assert list(validator.iter_errors(bad))


# ---------------------------------------------------------------------------
# happy path
# ---------------------------------------------------------------------------

@pytest.mark.asyncio
async def test_optimize_200_schema_valid_and_echo(client: AsyncClient) -> None:
    req = make_request(request_id="req_router000001")
    resp = await client.post(
        "/v1/optimize", json=req.model_dump(mode="json"), headers={"X-Request-Id": req.request_id},
    )
    assert resp.status_code == 200
    assert resp.headers.get("x-request-id") == req.request_id
    body = resp.json()
    parsed = OptimizeResponse.model_validate(body)
    assert parsed.request_id == req.request_id
    assert parsed.solver.status in ("OPTIMAL", "FEASIBLE")
    # one candidate in the fixture -> one roster, the rest are dropped, never duplicated
    assert len(parsed.variants) == 1
    assert parsed.diagnostics.dropped_diversity_variants == req.k - 1
    assert parsed.diagnostics.model_version == "opt-v2"
    assert re.fullmatch(r"sha256:[a-f0-9]{64}", parsed.deterministic_fingerprint)
    assert parsed.variants[0].reason_codes == ["VARIANT_BALANCED"]
    _assert_contract_valid(body)


@pytest.mark.asyncio
async def test_optimize_diverse_variants_with_labels(client: AsyncClient) -> None:
    req = build_request_from_scenario("small_competing_01")
    resp = await client.post(
        "/v1/optimize", json=req.model_dump(mode="json"), headers={"X-Request-Id": req.request_id},
    )
    assert resp.status_code == 200
    body = resp.json()
    variants = body["variants"]
    assert len(variants) >= 2
    rosters = [frozenset(m["employee_id"] for m in v["members"]) for v in variants]
    for a, b in itertools.combinations(rosters, 2):
        union = a | b
        distance = 1.0 - len(a & b) / len(union) if union else 0.0
        assert distance >= req.min_jaccard_diversity - 1e-9
    labels = [v["label"] for v in variants]
    assert all(label for label in labels)
    for v in variants:
        assert len(v["reason_codes"]) == 1
        assert v["reason_codes"][0] in {
            "VARIANT_FASTEST", "VARIANT_CHEAPEST", "VARIANT_LOWEST_RISK", "VARIANT_BALANCED",
        }
        for m in v["members"]:
            assert m["reason_codes"], "members must carry reason codes"
            assert all(code in VALID_REASON_CODES for code in m["reason_codes"])
    _assert_contract_valid(body)


@pytest.mark.asyncio
async def test_optimize_deficit_variant_response(client: AsyncClient) -> None:
    """allow_deficits=true (default): best partial team + explicit, schema-valid deficits."""
    req = build_request_from_scenario("small_deficit_01")
    resp = await client.post(
        "/v1/optimize", json=req.model_dump(mode="json"), headers={"X-Request-Id": req.request_id},
    )
    assert resp.status_code == 200
    body = resp.json()
    assert body["solver"]["status"] in ("OPTIMAL", "FEASIBLE")
    assert body["variants"]
    assert "DEFICIT_MUST_ROLE" in body["reason_codes"]
    for variant in body["variants"]:
        assert variant["deficits"], "a deficit scenario must list its deficits"
        for deficit in variant["deficits"]:
            # competency_id is required-and-nullable: the key must always be present
            assert "competency_id" in deficit
            assert deficit["person_days"] > 0
            assert deficit["external_cooperation_flag"] is True
            assert deficit["reason_codes"]
            assert all(code in VALID_REASON_CODES for code in deficit["reason_codes"])
        assert any(c["type"] == "capacity" for c in variant["active_constraints"])
    _assert_contract_valid(body)


@pytest.mark.asyncio
async def test_optimize_role_level_deficit_keeps_null_competency_key(client: AsyncClient) -> None:
    """Role-only shortage: ``competency_id`` stays present as ``null`` in the payload."""
    req = make_request(
        request_id="req_routernull01",
        requirements=[
            # staffable work so a roster exists
            dict(
                requirement_id="req_0100", role_id="backend_engineer", competency_id="lang_go",
                level=3, criticality="must", headcount=1,
                effort=dict(min_person_days=5, likely_person_days=10, max_person_days=15, unit="person_days"),
            ),
            dict(
                requirement_id="req_0101", role_id="ml_engineer", competency_id=None,
                level=3, criticality="must", headcount=1,
                effort=dict(min_person_days=5, likely_person_days=10, max_person_days=15, unit="person_days"),
            ),
        ],
    )
    resp = await client.post(
        "/v1/optimize", json=req.model_dump(mode="json"), headers={"X-Request-Id": req.request_id},
    )
    assert resp.status_code == 200
    body = resp.json()
    deficits = [d for v in body["variants"] for d in v["deficits"]]
    role_level = [d for d in deficits if d["competency_id"] is None]
    assert role_level, "expected a role-level deficit"
    assert role_level[0]["role_id"] == "ml_engineer"
    _assert_contract_valid(body)


@pytest.mark.asyncio
async def test_optimize_allow_deficits_false_is_infeasible(client: AsyncClient) -> None:
    req = build_request_from_scenario("small_deficit_01", allow_deficits=False)
    resp = await client.post(
        "/v1/optimize", json=req.model_dump(mode="json"), headers={"X-Request-Id": req.request_id},
    )
    assert resp.status_code == 200
    body = resp.json()
    parsed = OptimizeResponse.model_validate(body)
    assert parsed.solver.status == "INFEASIBLE"
    assert parsed.variants == []
    assert parsed.infeasibility is not None
    assert parsed.infeasibility.blocking_constraints
    assert parsed.infeasibility.deficits
    assert "SOLVER_INFEASIBLE" in body["reason_codes"]
    _assert_contract_valid(body)


@pytest.mark.asyncio
async def test_optimize_degraded_timeout_returns_best_found(
    client: AsyncClient, monkeypatch
) -> None:
    """Graceful timeout: the objective-guided solve finds nothing -> FEASIBLE + SOLVER_TIMEOUT.

    The response must carry the objective-free feasible team (never an empty variants list),
    must not invent an objective value, and must stay contract-valid.
    """
    from ortools.sat.python import cp_model

    from app.optimize.solver import _Model

    real_solve_once = _Model.solve_once

    def fake_solve_once(self, request, time_limit_ms):
        if not self.objective_cleared:
            return cp_model.UNKNOWN, 3
        return real_solve_once(self, request, time_limit_ms)

    monkeypatch.setattr(_Model, "solve_once", fake_solve_once)

    req = make_request(request_id="req_routerdeg001")
    resp = await client.post(
        "/v1/optimize", json=req.model_dump(mode="json"), headers={"X-Request-Id": req.request_id},
    )
    assert resp.status_code == 200
    body = resp.json()
    assert body["solver"]["status"] == "FEASIBLE"
    assert "objective_value" not in body["solver"], "a degraded plan must not claim an objective"
    assert "SOLVER_TIMEOUT" in body["reason_codes"]
    assert body["variants"] and body["variants"][0]["members"]
    _assert_contract_valid(body)


# ---------------------------------------------------------------------------
# errors
# ---------------------------------------------------------------------------

@pytest.mark.asyncio
async def test_optimize_422_semantic_error(client: AsyncClient) -> None:
    req = make_request()
    data = req.model_dump(mode="json")
    data["constraints"]["force_include"] = ["emp_nope"]  # unknown candidate -> 422
    resp = await client.post("/v1/optimize", json=data, headers={"X-Request-Id": req.request_id})
    assert resp.status_code == 422
    body = resp.json()
    assert body["code"] == "VALIDATION_FAILED"
    assert body["retryable"] is False
    assert "VALIDATION_FAILED" in body["reason_codes"]


@pytest.mark.asyncio
async def test_optimize_rejects_invalid_k(client: AsyncClient) -> None:
    req = make_request()
    data = req.model_dump(mode="json")
    data["k"] = 1
    resp = await client.post("/v1/optimize", json=data, headers={"X-Request-Id": req.request_id})
    assert resp.status_code == 422
