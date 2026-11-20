import { useEffect, useState } from 'react';
import { Button, Card, List, Progress, Space, Tag, Typography, message } from 'antd';
import { useParams, useNavigate } from 'react-router-dom';
import { confirmBrief, getRequirements } from '../api/client';
import type { Requirement } from '../types';

export default function BriefReviewPage() {
  const { briefId = '' } = useParams();
  const navigate = useNavigate();
  const [items, setItems] = useState<Requirement[]>([]);

  useEffect(() => {
    getRequirements(briefId).then(setItems);
  }, [briefId]);

  const confirmAndConfigure = async () => {
    await confirmBrief(briefId);
    message.success('Требования подтверждены');
    navigate(`/runs/config/${briefId}`);
  };

  return (
    <Space direction="vertical" size="large" style={{ width: '100%' }}>
      <Typography.Title level={2}>Проверка требований</Typography.Title>
      <Card
        extra={
          <Space>
            <Button type="primary" onClick={confirmAndConfigure}>
              Подтвердить и настроить расчёт
            </Button>
          </Space>
        }
      >
        <List
          dataSource={items}
          locale={{ emptyText: 'Требования не найдены' }}
          renderItem={(r) => (
            <List.Item>
              <List.Item.Meta
                title={`${r.role.name_ru} — ${r.competency_name_ru}`}
                description={r.evidence.map((e) => `«${e.quote}»`).join('; ')}
              />
              <Space>
                <Progress type="circle" size={42} percent={Math.round(r.confidence * 100)} />
                <Tag>{r.criticality}</Tag>
              </Space>
            </List.Item>
          )}
        />
      </Card>
    </Space>
  );
}
