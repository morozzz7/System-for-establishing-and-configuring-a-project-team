/**
 * Typed API error built from the contract `Problem` shape (RFC 9457 + reason codes).
 * The UI renders `code`, `detail` and `reason_codes`; raw stacks/servers messages never reach it.
 */
import type { FieldError, Problem, ReasonCode } from '../types';

export interface ApiErrorInit {
  status: number;
  code: string;
  detail: string;
  reasonCodes?: ReasonCode[];
  fieldErrors?: FieldError[];
  requestId?: string;
  retryable?: boolean;
  title?: string;
}

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly detail: string;
  readonly reasonCodes: ReasonCode[];
  readonly fieldErrors: FieldError[];
  readonly requestId?: string;
  readonly retryable: boolean;
  readonly title?: string;

  constructor(init: ApiErrorInit) {
    super(init.detail);
    this.name = 'ApiError';
    this.status = init.status;
    this.code = init.code;
    this.detail = init.detail;
    this.reasonCodes = init.reasonCodes ?? [];
    this.fieldErrors = init.fieldErrors ?? [];
    this.requestId = init.requestId;
    this.retryable = init.retryable ?? false;
    this.title = init.title;
  }

  /** Build from a parsed `Problem` body. */
  static fromProblem(problem: Problem, fallbackStatus: number): ApiError {
    return new ApiError({
      status: problem.status ?? fallbackStatus,
      code: problem.code || 'UNKNOWN_ERROR',
      detail: problem.detail || problem.title || `API ${fallbackStatus}`,
      reasonCodes: problem.reason_codes ?? [],
      fieldErrors: problem.errors ?? [],
      requestId: problem.request_id,
      retryable: problem.retryable,
      title: problem.title,
    });
  }

  static network(detail: string): ApiError {
    return new ApiError({ status: 0, code: 'NETWORK_ERROR', detail, retryable: true });
  }
}

export function isApiError(error: unknown): error is ApiError {
  return error instanceof ApiError;
}

/** Normalize anything thrown by the client into an ApiError (no stack ever surfaces in the UI). */
export function toApiError(error: unknown): ApiError {
  if (isApiError(error)) return error;
  if (error instanceof Error) {
    return new ApiError({ status: 0, code: 'CLIENT_ERROR', detail: error.message });
  }
  return new ApiError({ status: 0, code: 'CLIENT_ERROR', detail: 'Неизвестная ошибка' });
}

/** Human-readable Russian summary of a reason code list (i18n-ready: one map, no scattered literals). */
const REASON_CODE_LABELS: Partial<Record<ReasonCode, string>> = {
  // recalculation
  RECALC_APPLIED: 'Пересчёт применён',
  RECALC_NO_EFFECT: 'Изменений нет',
  RECALC_QUEUED: 'Пересчёт поставлен в очередь',
  OVERRIDE_REJECTED: 'Ограничение отклонено',
  OVERRIDE_VIOLATES_G1: 'Нарушает G1: сотрудник недоступен',
  OVERRIDE_VIOLATES_G2: 'Нарушает G2: перегрузка',
  OVERRIDE_VIOLATES_G4: 'Нарушает G4: пользовательское ограничение',
  VALIDATION_FAILED: 'Ошибка валидации',
  SOLVER_INFEASIBLE: 'Совместимый состав не найден',
  // inclusion
  INCLUDE_REQUIRED_ROLE: 'Необходимая роль',
  INCLUDE_BENCH: 'На скамейке',
  INCLUDE_USER_FORCE: 'Включён пользователем',
  INCLUDE_HIGH_COMPETENCE: 'Высокая компетенция',
  INCLUDE_AVAILABLE_CAPACITY: 'Есть свободная ёмкость',
  INCLUDE_COST_EFFICIENT: 'Экономичный',
  INCLUDE_BUS_FACTOR: 'Усиливает bus factor',
  INCLUDE_EXPERIENCE_RELEVANT: 'Релевантный опыт',
  // exclusion
  EXCLUDE_USER_FORCE: 'Исключён пользователем',
  EXCLUDE_OVERLOADED: 'Перегружен',
  EXCLUDE_ABSENT: 'Отсутствует / недоступен',
  EXCLUDE_BELOW_REQUIRED_LEVEL: 'Ниже требуемого уровня',
  EXCLUDE_EXPENSIVE: 'Дорогой',
  EXCLUDE_TEAM_SIZE_LIMIT: 'Ограничение размера команды',
  EXCLUDE_NO_FREE_CAPACITY: 'Нет свободной ёмкости',
  EXCLUDE_PORTFOLIO_PROTECTION: 'Защита портфеля',
  EXCLUDE_DATA_INCOMPLETE: 'Неполные данные',
  // deficits
  DEFICIT_MUST_ROLE: 'Дефицит must-роли',
  DEFICIT_EXTERNAL_CONTRACTOR: 'Нужен подрядчик',
  DEFICIT_UPSKILL_INTERNAL: 'Нужно обучение внутри',
  DEFICIT_DEADLINE_SHIFT: 'Сдвиг срока',
  DEFICIT_DESCOPE: 'Уменьшение объёма',
  // constraints
  CONSTRAINT_ACTIVE_TEAM_SIZE: 'Активно: размер команды',
  CONSTRAINT_ACTIVE_BUDGET: 'Активно: бюджет',
  CONSTRAINT_ACTIVE_DEADLINE: 'Активно: срок',
  CONSTRAINT_ACTIVE_FORCE_INCLUDE: 'Активно: принудительное включение',
  CONSTRAINT_ACTIVE_FORCE_EXCLUDE: 'Активно: принудительное исключение',
  CONSTRAINT_ACTIVE_BUS_FACTOR: 'Активно: bus factor',
  CONSTRAINT_BINDING_CAPACITY: 'Ограничение по ёмкости',
  // variants
  VARIANT_FASTEST: 'Самый быстрый вариант',
  VARIANT_CHEAPEST: 'Самый экономичный вариант',
  VARIANT_LOWEST_RISK: 'Вариант с низким риском',
  VARIANT_BALANCED: 'Сбалансированный вариант',
  VARIANT_DOMINATED_REPLACED: 'Доминируемый вариант заменён',
  // tradeoffs / counterfactuals
  TRADEOFF_SPEED_VS_DEFICIT: 'Скорость против дефицита',
  TRADEOFF_COST_VS_SKILL: 'Стоимость против навыков',
  TRADEOFF_LOAD_VS_SPEED: 'Загрузка против скорости',
  COUNTERFACTUAL_TEAM_SIZE: 'Гипотеза: размер команды',
  COUNTERFACTUAL_DEADLINE: 'Гипотеза: срок',
  COUNTERFACTUAL_CAPACITY: 'Гипотеза: ёмкость',
  COUNTERFACTUAL_OVERRIDE: 'Гипотеза: ограничение',
};

export function reasonCodeLabel(code: ReasonCode): string {
  return REASON_CODE_LABELS[code] ?? code;
}
