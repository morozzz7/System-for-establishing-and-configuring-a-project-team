/**
 * "Почему этот состав" — explanation for the selected variant (task 4.4/4.3).
 *
 * Renders the headline + its reason codes, the main trade-off, and collapsible lists of
 * included / excluded / deficit factors (each with Russian text + reason-code tags).
 * Every counterfactual is labelled by how it was produced: `rerun` (проверено пересчётом)
 * or `estimated` (оценка, не пересчитано) — human-in-the-loop, no unproven exactness.
 */
import { Alert, Card, Collapse, Divider, List, Space, Spin, Tag, Typography } from 'antd';
import type { Counterfactual, Explanation, ExplanationFactor } from '../types';
import { reasonCodeLabel } from '../api/errors';

const { Text, Paragraph } = Typography;

function FactorList({
  factors,
  localeEmpty,
}: {
  factors: ExplanationFactor[];
  localeEmpty: string;
}) {
  if (factors.length === 0) {
    return <Text type="secondary">{localeEmpty}</Text>;
  }
  return (
    <List
      size="small"
      dataSource={factors}
      renderItem={(factor) => (
        <List.Item>
          <Space direction="vertical" size={2} style={{ width: '100%' }}>
            <Space wrap size={[6, 2]}>
              {factor.employee_id && <Tag color="default">{factor.employee_id}</Tag>}
              {factor.competency_id && <Tag color="cyan">{factor.competency_id}</Tag>}
            </Space>
            <Text>{factor.text}</Text>
            <Space wrap size={[4, 4]}>
              {(factor.reason_codes ?? []).map((code) => (
                <Tag key={code} color="blue">
                  {reasonCodeLabel(code)}
                </Tag>
              ))}
            </Space>
          </Space>
        </List.Item>
      )}
    />
  );
}

function CounterfactualLabel({ item }: { item: Counterfactual }) {
  if (item.verified_by === 'rerun') {
    return (
      <Tag color="green">{`проверено пересчётом (run ${item.run_id ?? '—'})`}</Tag>
    );
  }
  return <Tag color="orange">оценка (не пересчитано)</Tag>;
}

export default function ExplanationPanel({ explanation }: { explanation: Explanation }) {
  // A malformed API response must not blank the whole screen while a human is deciding.
  const included = explanation.included ?? [];
  const excluded = explanation.excluded ?? [];
  const deficits = explanation.deficits ?? [];
  const counterfactuals = explanation.counterfactuals ?? [];
  const headlineCodes = explanation.headline_reason_codes ?? [];
  const tradeoffCodes = explanation.tradeoff?.reason_codes ?? [];

  return (
    <Card size="small" title="Почему этот состав">
      <Space direction="vertical" size="small" style={{ width: '100%' }}>
        <Space direction="vertical" size={4} style={{ width: '100%' }}>
          <Text strong>Итог</Text>
          <Paragraph style={{ marginBottom: 4 }}>{explanation.headline}</Paragraph>
          <Space wrap size={[4, 4]}>
            {headlineCodes.map((code) => (
              <Tag key={code} color="geekblue">
                {reasonCodeLabel(code)}
              </Tag>
            ))}
          </Space>
        </Space>

        <Divider style={{ margin: '4px 0' }} />

        <Alert
          type="info"
          showIcon
          message="Компромисс"
          description={explanation.tradeoff?.text ?? ''}
        />
        <Space wrap size={[4, 4]}>
          {tradeoffCodes.map((code) => (
            <Tag key={code} color="processing">
              {reasonCodeLabel(code)}
            </Tag>
          ))}
        </Space>

        <Divider style={{ margin: '4px 0' }} />

        <Collapse
          size="small"
          defaultActiveKey={['included', 'excluded', 'deficits']}
          items={[
            {
              key: 'included',
              label: `Включены (${included.length})`,
              children: (
                <FactorList factors={included} localeEmpty="Участники не описаны." />
              ),
            },
            {
              key: 'excluded',
              label: `Исключены (${excluded.length})`,
              children: (
                <FactorList factors={excluded} localeEmpty="Исключённых нет." />
              ),
            },
            {
              key: 'deficits',
              label: `Дефициты (${deficits.length})`,
              children: (
                <FactorList factors={deficits} localeEmpty="Дефицитов нет." />
              ),
            },
          ]}
        />

        <Divider style={{ margin: '4px 0' }} />

        <Text strong>Что если…</Text>
        {counterfactuals.length === 0 ? (
          <Text type="secondary">Контрфактических сценариев нет.</Text>
        ) : (
          <List
            size="small"
            dataSource={counterfactuals}
            renderItem={(item) => (
              <List.Item>
                <Space direction="vertical" size={2}>
                  <Text>{item.text}</Text>
                  <Space wrap size={[4, 4]}>
                    <CounterfactualLabel item={item} />
                    {(item.reason_codes ?? []).map((code) => (
                      <Tag key={code}>{reasonCodeLabel(code)}</Tag>
                    ))}
                  </Space>
                </Space>
              </List.Item>
            )}
          />
        )}
      </Space>
    </Card>
  );
}

/** Loading / missing-state for the explanation while the variant detail is fetched. */
export function ExplanationLoading() {
  return (
    <Card size="small" title="Почему этот состав">
      <Spin size="small">
        <div data-testid="explanation-loading-hint">Загружаем обоснование состава...</div>
      </Spin>
    </Card>
  );
}