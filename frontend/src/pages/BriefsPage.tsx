import { useEffect, useState } from 'react';
import { Button, Card, Form, Input, List, Space, Tag, Typography, message } from 'antd';
import { createBrief, listBriefs } from '../api/client';
import type { Brief } from '../types';
import { Link } from 'react-router-dom';

export default function BriefsPage() {
  const [briefs, setBriefs] = useState<Brief[]>([]); const [form] = Form.useForm();
  const refresh = () => listBriefs().then(setBriefs);
  useEffect(() => { void refresh(); }, []);
  const submit = async (v: { title: string; text: string }) => {
    try {
      await createBrief(v);
      form.resetFields();
      await refresh();
      message.success('ТЗ отправлено на извлечение');
    } catch (error) {
      message.error(error instanceof Error ? error.message : 'Не удалось извлечь требования');
    }
  };
  return <Space direction="vertical" size="large" style={{ width: '100%' }}>
    <Typography.Title level={2}>Технические задания</Typography.Title>
    <Card title="Новое ТЗ"><Form form={form} layout="vertical" onFinish={submit}>
      <Form.Item name="title" label="Название" rules={[{ required: true }]}><Input /></Form.Item>
      <Form.Item name="text" label="Текст ТЗ" rules={[{ required: true, min: 20 }]}><Input.TextArea rows={8} /></Form.Item>
      <Button type="primary" htmlType="submit">Извлечь требования</Button>
    </Form></Card>
    <List header="Последние ТЗ" dataSource={briefs} locale={{ emptyText: 'Пока нет загруженных ТЗ' }} renderItem={(b) => <List.Item><List.Item.Meta title={<Link to={`/briefs/${b.brief_id}/review`}>{b.title}</Link>} description={b.brief_id} /><Tag color={b.status === 'needs_review' ? 'orange' : 'green'}>{b.status}</Tag></List.Item>} />
  </Space>;
}
