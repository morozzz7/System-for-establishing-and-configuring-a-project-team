import type { Brief, Requirement } from '../types';

const scenarios = [
  {
    "title": "Реестр заявок на обслуживание",
    "body": "Backend-разработчик уровня 3 по Go должен сделать API регистрации заявок и выдачи истории статусов. Приёмка: оператор создаёт заявку, меняет её статус и выгружает журнал изменений. Оценка 5–9 человеко-дней, вероятнее 7. Предполагается готовая схема данных и существующая авторизация.",
    "start": "2026-W49",
    "end": "2026-W51",
    "role": "backend_engineer",
    "roleName": "Backend-разработчик",
    "competency": "lang_go",
    "competencyName": "Go",
    "level": 3
  },
  {
    "title": "Рабочее место диспетчера",
    "body": "Frontend-разработчик уровня 4 по React должен сделать очередь заявок с фильтрами и карточку с журналом статусов. Приёмка: диспетчер находит срочные заявки по приоритету и открывает историю. Оценка 5–9 человеко-дней, вероятнее 7. API и макеты доступны к старту.",
    "start": "2026-W50",
    "end": "2026-W51",
    "role": "frontend_engineer",
    "roleName": "Frontend-разработчик",
    "competency": "fw_react",
    "competencyName": "React",
    "level": 4
  },
  {
    "title": "Проверка безопасности партнёрского кабинета",
    "body": "Специалист уровня 5 по безопасности приложений должен проверить права доступа партнёров и подготовить отчёт с результатами проверки. Приёмка: подтверждена изоляция данных организаций, для замечаний указаны приоритеты. Оценка 5–9 человеко-дней, вероятнее 7. Тестовый стенд доступен к старту.",
    "start": "2026-W49",
    "end": "2026-W51",
    "role": "security_engineer",
    "roleName": "Специалист по безопасности",
    "competency": "sec_appsec",
    "competencyName": "Безопасность приложений",
    "level": 5
  }
] as const;

export const demoBriefs: Brief[] = scenarios.map((scenario, index) => ({
  brief_id: `brf_demo000${index + 1}`,
  title: scenario.title,
  source_type: 'text',
  language: 'ru',
  status: 'confirmed',
  created_at: '2026-11-27T09:00:00Z',
  requirement_count: 1,
  needs_review_count: 0,
  reason_codes: [],
  requested_start_week: scenario.start,
  deadline_week: scenario.end,
}));

export const demoRequirements: Record<string, Requirement[]> = Object.fromEntries(
  demoBriefs.map((brief, index) => {
    const scenario = scenarios[index];
    return [brief.brief_id, [{
      requirement_id: `req_demo000${index + 1}`,
      brief_id: brief.brief_id,
      role: { role_id: scenario.role, name_ru: scenario.roleName, confidence: 1 },
      competency_id: scenario.competency,
      competency_name_ru: scenario.competencyName,
      level: scenario.level,
      criticality: 'must',
      headcount: 1,
      confidence: 1,
      needs_review: false,
      effort: { min_person_days: 5, likely_person_days: 7, max_person_days: 9, unit: 'person_days' },
      assumptions: [scenario.body.split('. ').pop() ?? scenario.body],
      evidence: [{ kind: 'brief_span', quote: scenario.body }],
    }]];
  }),
);
