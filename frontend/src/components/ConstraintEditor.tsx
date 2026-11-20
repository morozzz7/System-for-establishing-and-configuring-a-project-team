/**
 * Constraint editor for recalculation (task 4.4).
 *
 * - Shows the current constraints of the run from the immutable input snapshot (GET /runs/{id}/inputs).
 * - Collects `ConstraintOverride` items, validates them client-side and blocks the action when the
 *   manager would manually break a hard constraint (AGENTS.md rule 6, METRICS G1/G4).
 * - Posts `RecalculateRequest`; the server always creates a NEW run (ADR-007 §3), so the current run
 *   and its snapshot are never mutated.
 */
import { useMemo, useState } from 'react';
import {
  Alert,
  AutoComplete,
  Button,
  Divider,
  Drawer,
  Empty,
  Input,
  InputNumber,
  List,
  Select,
  Space,
  Switch,
  Tag,
  Typography,
} from 'antd';
import type { ConstraintOverride, ConstraintType, RecalculateResponse, RunInputs, TeamRun } from '../types';
import { ApiError, toApiError } from '../api/errors';
import { getRun, recalculateRun } from '../api/client';
import {
  CONSTRAINT_TYPE_HINTS,
  CONSTRAINT_TYPE_LABELS,
  REASON_MAX_LENGTH,
  buildRecalculateRequest,
  candidatesWithoutCapacity,
  constraintRows,
  describeOverride,
  draftFieldsForType,
  draftForType,
  emptyDraft,
  hasBlockingIssues,
  normalizeOverride,
  partiallyUnavailableCandidateIds,
  validateOverride,
  validateOverrides,
} from '../utils/constraints';
import type { OverrideDraft, OverrideIssue, OverrideValidationContext } from '../utils/constraints';

const { Text, Paragraph } = Typography;

const CONSTRAINT_TYPES = Object.keys(CONSTRAINT_TYPE_LABELS) as ConstraintType[];

export interface ConstraintEditorProps {
  open: boolean;
  run: TeamRun | null;
  inputs: RunInputs | null;
  inputsError: string | null;
  baseVariantId?: string;
  onClose: () => void;
  onRecalculated: (response: RecalculateResponse, newRun: TeamRun) => void;
  onError: (error: ApiError) => void;
}

export default function ConstraintEditor({
  open,
  run,
  inputs,
  inputsError,
  baseVariantId,
  onClose,
  onRecalculated,
  onError,
}: ConstraintEditorProps) {
  const [draft, setDraft] = useState<OverrideDraft>(() => emptyDraft('force_exclude'));
  const [overrides, setOverrides] = useState<ConstraintOverride[]>([]);
  const [formIssues, setFormIssues] = useState<OverrideIssue[]>([]);
  const [submitting, setSubmitting] = useState(false);

  const horizon = inputs?.horizon ?? run?.horizon;
  const context = useMemo<OverrideValidationContext>(
    () => ({
      candidateIds: inputs?.candidates.map((candidate) => candidate.employee_id),
      unavailableCandidateIds: partiallyUnavailableCandidateIds(inputs?.candidates, horizon),
      fullyUnavailableCandidateIds: candidatesWithoutCapacity(inputs?.candidates),
      horizon,
    }),
    [inputs, horizon],
  );

  const listIssues = useMemo(() => validateOverrides(overrides, context), [overrides, context]);
  const blocked = hasBlockingIssues(listIssues);

  const employeeOptions = useMemo(
    () =>
      (inputs?.candidates ?? []).map((candidate) => ({
        value: candidate.employee_id,
        label: `${candidate.employee_id} · ${candidate.grade}`,
      })),
    [inputs],
  );

  const fields = draftFieldsForType(draft.type);
  const draftOverride = normalizeOverride(draft);

  const updateDraft = (patch: Partial<OverrideDraft>) => {
    setDraft((previous) => ({ ...previous, ...patch }));
    setFormIssues([]);
  };

  const changeType = (type: ConstraintType) => {
    setDraft((previous) => draftForType(type, previous));
    setFormIssues([]);
  };

  const addOverride = () => {
    const candidate = normalizeOverride(draft);
    const fieldIssues = validateOverride(candidate, context);
    const duplicateIssues = validateOverrides([...overrides, candidate], context).filter(
      (issue) => issue.index === overrides.length && issue.message.startsWith('Дубликат'),
    );
    const allIssues = [...fieldIssues, ...duplicateIssues];
    // Warnings (partially available employee, missing from snapshot) may be added; contradictions may not.
    if (hasBlockingIssues(allIssues)) {
      setFormIssues(allIssues);
      return;
    }
    setOverrides((previous) => [...previous, candidate]);
    setDraft(draftForType(draft.type, draft));
    setFormIssues([]);
  };

  const removeOverride = (index: number) => {
    setOverrides((previous) => previous.filter((_, position) => position !== index));
    setFormIssues([]);
  };

  const submit = async () => {
    if (!run || overrides.length === 0) return;
    if (hasBlockingIssues(validateOverrides(overrides, context))) return;
    setSubmitting(true);
    try {
      const response = await recalculateRun(run.run_id, buildRecalculateRequest(run, baseVariantId, overrides));
      const newRun = await getRun(response.run_id);
      if (!newRun) {
        throw new ApiError({ status: 404, code: 'NOT_FOUND', detail: 'Новый запуск не найден', reasonCodes: [] });
      }
      onRecalculated(response, newRun);
      setOverrides([]);
      setFormIssues([]);
    } catch (error) {
      onError(toApiError(error));
    } finally {
      setSubmitting(false);
    }
  };

  const currentRows = constraintRows(inputs?.constraints);

  return (
    <Drawer
      title="Ограничения и пересчёт"
      width={560}
      open={open}
      onClose={onClose}
      destroyOnClose={false}
      footer={
        <Space style={{ display: 'flex', justifyContent: 'flex-end' }}>
          <Button
            onClick={() => {
              setOverrides([]);
              setFormIssues([]);
            }}
            disabled={overrides.length === 0 || submitting}
          >
            Очистить
          </Button>
          <Button type="primary" onClick={submit} loading={submitting} disabled={overrides.length === 0 || blocked}>
            Пересчитать
          </Button>
        </Space>
      }
    >
      <Space direction="vertical" size="middle" style={{ width: '100%' }}>
        <Alert
          type="info"
          showIcon
          message="Будет создан новый запуск, текущий не изменится"
          description="Исходный запуск и снимок входа остаются неизменными; новый запуск связан с ним как дочерний (ADR-007 §3)."
        />

        <div>
          <Text strong>Текущие ограничения запуска</Text>
          {inputsError && (
            <Alert style={{ marginTop: 8 }} type="warning" showIcon message="Не удалось загрузить снимок входа" description={inputsError} />
          )}
          {!inputsError && currentRows.length === 0 && (
            <Paragraph type="secondary" style={{ marginTop: 8, marginBottom: 0 }}>
              Ограничения не заданы — действуют только жёсткие правила доступности и загрузки.
            </Paragraph>
          )}
          {currentRows.length > 0 && (
            <Space direction="vertical" size={4} style={{ marginTop: 8, width: '100%' }}>
              {currentRows.map((row) => (
                <Space key={row.key} wrap>
                  <Tag>{row.label}</Tag>
                  <Text>{row.value}</Text>
                </Space>
              ))}
            </Space>
          )}
          {inputs && (
            <Paragraph type="secondary" style={{ marginTop: 8, marginBottom: 0, fontSize: 12 }}>
              Снимок {inputs.snapshot_id}: {inputs.candidate_count} кандидатов, {inputs.capacity_rows} строк
              доступности, sha256 {inputs.snapshot_sha256.slice(0, 12)}…
            </Paragraph>
          )}
        </div>

        <Divider style={{ margin: 0 }} />

        <Space direction="vertical" size="small" style={{ width: '100%' }}>
          <Text strong>Новое ограничение</Text>
          <Space wrap>
            <Select
              aria-label="Тип ограничения"
              value={draft.type}
              onChange={changeType}
              style={{ minWidth: 240 }}
              options={CONSTRAINT_TYPES.map((type) => ({ value: type, label: CONSTRAINT_TYPE_LABELS[type] }))}
            />
            <Text type="secondary" style={{ fontSize: 12 }}>
              {CONSTRAINT_TYPE_HINTS[draft.type]}
            </Text>
          </Space>

          {fields.employee && (
            <AutoComplete
              aria-label="Сотрудник"
              value={draft.employee_id}
              onChange={(value) => updateDraft({ employee_id: value })}
              options={employeeOptions}
              placeholder="emp_0001"
              style={{ width: '100%' }}
              filterOption={(input, option) =>
                String(option?.value ?? '').toLowerCase().includes(input.toLowerCase())
              }
            />
          )}

          {fields.employeeList && (
            <Select
              aria-label="Закрепляемые сотрудники"
              mode="tags"
              value={draft.employee_ids}
              onChange={(value: string[]) => updateDraft({ employee_ids: value })}
              options={employeeOptions}
              placeholder="emp_0001, emp_0002"
              style={{ width: '100%' }}
              tokenSeparators={[',', ' ']}
            />
          )}

          {fields.number && (
            <Space>
              <Text>{fields.numberLabel}</Text>
              <InputNumber
                aria-label={fields.numberLabel ?? 'Значение'}
                min={fields.numberMin}
                max={fields.numberMax}
                step={fields.numberStep ?? 1}
                value={draft.value}
                onChange={(value) => updateDraft({ value: value === null ? null : Number(value) })}
              />
            </Space>
          )}

          {fields.week && (
            <Input
              aria-label="Неделя срока"
              placeholder="2026-W14"
              value={draft.week}
              onChange={(event) => updateDraft({ week: event.target.value })}
              status={draft.week.length > 0 && draftOverride.type === 'deadline' && hasBlockingIssues(validateOverride(draftOverride, context)) ? 'error' : undefined}
            />
          )}

          {fields.booleanFlag && (
            <Space>
              <Switch
                aria-label="Запретить внешних"
                checked={draft.flag}
                onChange={(checked) => updateDraft({ flag: checked })}
              />
              <Text>{draft.flag ? 'Внешние подрядчики запрещены' : 'Внешние подрядчики разрешены'}</Text>
            </Space>
          )}

          <Input.TextArea
            aria-label="Обоснование"
            placeholder="Обоснование (необязательно), до 300 символов"
            value={draft.reason}
            onChange={(event) => updateDraft({ reason: event.target.value })}
            maxLength={REASON_MAX_LENGTH}
            showCount
            rows={2}
          />

          {formIssues.length > 0 && (
            <Alert
              type="warning"
              showIcon
              message="Ограничение не добавлено"
              description={
                <ul style={{ margin: 0, paddingLeft: 18 }}>
                  {formIssues.map((issue) => (
                    <li key={`${issue.index}-${issue.field}-${issue.message}`}>{issue.message}</li>
                  ))}
                </ul>
              }
            />
          )}

          <Button onClick={addOverride} aria-label="Добавить ограничение">
            Добавить ограничение
          </Button>

          <Divider style={{ margin: '8px 0' }} />

          <Text strong>Добавленные ограничения ({overrides.length})</Text>
          {overrides.length === 0 ? (
            <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="Пока не добавлено ни одного ограничения" />
          ) : (
            <List
              size="small"
              dataSource={overrides}
              renderItem={(override, index) => (
                <List.Item
                  actions={[
                    <Button key="remove" size="small" danger onClick={() => removeOverride(index)} aria-label={`Удалить ограничение ${index + 1}`}>
                      Удалить
                    </Button>,
                  ]}
                >
                  <List.Item.Meta
                    title={<Text>{`${index + 1}. ${describeOverride(override)}`}</Text>}
                    description={
                      <Space direction="vertical" size={0}>
                        <Text type="secondary" style={{ fontSize: 12 }}>
                          {CONSTRAINT_TYPE_LABELS[override.type]}
                        </Text>
                        {override.reason && <Text type="secondary" style={{ fontSize: 12 }}>{override.reason}</Text>}
                      </Space>
                    }
                  />
                </List.Item>
              )}
            />
          )}

          {blocked && (
            <Alert
              type="error"
              showIcon
              message="Пересчёт заблокирован: ограничения нарушают жёсткие правила"
              description={
                <ul style={{ margin: 0, paddingLeft: 18 }}>
                  {listIssues
                    .filter((issue) => issue.blocking)
                    .map((issue) => (
                      <li key={`${issue.index}-${issue.field}-${issue.message}`}>
                        {`Ограничение ${issue.index + 1}: ${issue.message}`}
                      </li>
                    ))}
                </ul>
              }
            />
          )}
          {!blocked && listIssues.length > 0 && (
            <Alert
              type="warning"
              showIcon
              message="Предупреждение"
              description={
                <ul style={{ margin: 0, paddingLeft: 18 }}>
                  {listIssues.map((issue) => (
                    <li key={`${issue.index}-${issue.field}-${issue.message}`}>{issue.message}</li>
                  ))}
                </ul>
              }
            />
          )}
        </Space>
      </Space>
    </Drawer>
  );
}
