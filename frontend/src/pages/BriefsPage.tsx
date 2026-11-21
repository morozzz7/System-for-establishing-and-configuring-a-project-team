import { useEffect, useState, type ReactNode } from 'react';
import {
  Button,
  Card,
  Form,
  Input,
  Space,
  Tag,
  Typography,
  Upload,
  message,
  Table,
  Badge,
  Row,
  Col,
  Alert,
} from 'antd';
import {
  InboxOutlined,
  FileTextOutlined,
  SendOutlined,
  ExperimentOutlined,
  FileDoneOutlined,
  CloseCircleOutlined,
  CheckCircleOutlined,
  ClockCircleOutlined,
} from '@ant-design/icons';
import type { ColumnsType } from 'antd/es/table';
import { createBrief, createBriefFromFile, listBriefs } from '../api/client';
import type { Brief } from '../types';
import { Link, useNavigate } from 'react-router-dom';

const { Title, Text } = Typography;

const SAMPLE_BRIEF_TITLE = 'Миграция портала на Go и React';
const SAMPLE_BRIEF_TEXT = `Требуется перевести клиентский портал с монолита на микросервисную архитектуру.
Необходимо: 2 Senior бэкенд-разработчика со знанием Go, PostgreSQL, Kafka и Docker.
Один Middle фронтенд-разработчик на React и TypeScript для обновления личного кабинета.
Один QA-инженер для автоматизации сквозного регрессионного тестирования (Playwright/Go).
Один Team Lead с опытом архитектурного проектирования и управления распределенной командой.
Планируемый старт работ: 2026-W08, горизонт реализации: 12 недель, крайний срок: 2026-W20.`;

export default function BriefsPage() {
  const [briefs, setBriefs] = useState<Brief[]>([]);
  const [loading, setLoading] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [uploadedFile, setUploadedFile] = useState<File | null>(null);
  const [form] = Form.useForm();
  const navigate = useNavigate();

  const refresh = () => {
    setLoading(true);
    listBriefs()
      .then(setBriefs)
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    void refresh();
  }, []);

  const handleFileDrop = async (file: File) => {
    setUploadedFile(file);
    const suggestedTitle = file.name.replace(/\.[^/.]+$/, '').replace(/[_-]/g, ' ');
    form.setFieldsValue({ title: suggestedTitle });

    // If it's a text/markdown/json file, read content to textarea for live inspection
    if (
      file.type.startsWith('text/') ||
      file.name.endsWith('.txt') ||
      file.name.endsWith('.md') ||
      file.name.endsWith('.json')
    ) {
      try {
        const text = await file.text();
        form.setFieldsValue({ text });
        message.success(`Файл "${file.name}" прочитан (${text.length} симв.)`);
      } catch {
        message.warning('Не удалось отобразить текст файла, будет отправлен как вложение');
      }
    } else {
      message.info(`Файл "${file.name}" готов к отправке на серверное извлечение`);
    }
    return false;
  };

  const handleClearFile = () => {
    setUploadedFile(null);
    form.resetFields(['title', 'text']);
  };

  const handleLoadSample = () => {
    setUploadedFile(null);
    form.setFieldsValue({
      title: SAMPLE_BRIEF_TITLE,
      text: SAMPLE_BRIEF_TEXT,
    });
    message.success('Загружен образец технического задания');
  };

  const submit = async (values: { title: string; text?: string }) => {
    setSubmitting(true);
    try {
      let created: Brief;
      if (uploadedFile && (!values.text || uploadedFile.name.endsWith('.docx') || uploadedFile.name.endsWith('.pdf'))) {
        created = await createBriefFromFile(uploadedFile, values.title);
      } else {
        if (!values.text || values.text.trim().length < 20) {
          message.error('Текст ТЗ должен содержать не менее 20 символов');
          setSubmitting(false);
          return;
        }
        created = await createBrief({ title: values.title, text: values.text });
      }

      form.resetFields();
      setUploadedFile(null);
      await refresh();
      message.success(`ТЗ "${created.title}" успешно создано`);
      navigate(`/briefs/${created.brief_id}/review`);
    } catch (error) {
      message.error(error instanceof Error ? error.message : 'Не удалось обработать ТЗ');
    } finally {
      setSubmitting(false);
    }
  };

  const columns: ColumnsType<Brief> = [
    {
      title: 'Название проекта / ТЗ',
      dataIndex: 'title',
      key: 'title',
      render: (title: string, record: Brief) => (
        <Space direction="vertical" size={2}>
          <Link to={`/briefs/${record.brief_id}/review`} style={{ fontWeight: 600, color: '#60a5fa' }}>
            {title}
          </Link>
          <Text style={{ fontSize: 11, color: '#64748b' }}>{record.brief_id}</Text>
        </Space>
      ),
    },
    {
      title: 'Источник',
      dataIndex: 'source_type',
      key: 'source_type',
      width: 120,
      render: (src: string) => (
        <Tag color={src === 'document' ? 'purple' : 'blue'}>
          {src === 'document' ? 'Документ' : 'Текст'}
        </Tag>
      ),
    },
    {
      title: 'Статус',
      dataIndex: 'status',
      key: 'status',
      width: 150,
      render: (status: string) => {
        if (status === 'confirmed') return <Tag color="success" icon={<CheckCircleOutlined />}>Подтверждено</Tag>;
        if (status === 'needs_review') return <Tag color="warning" icon={<ClockCircleOutlined />}>Нужен аудит</Tag>;
        if (status === 'extracting') return <Tag color="processing">Извлечение...</Tag>;
        return <Tag color="default">{status}</Tag>;
      },
    },
    {
      title: 'Требований',
      key: 'requirements',
      width: 130,
      render: (_, record) => (
        <Space size="small">
          <Badge count={record.requirement_count ?? 0} style={{ backgroundColor: '#10b981' }} />
          {(record.needs_review_count ?? 0) > 0 && (
            <TooltipWrapper title="Требуют ручного подтверждения">
              <Badge count={record.needs_review_count} style={{ backgroundColor: '#f59e0b' }} />
            </TooltipWrapper>
          )}
        </Space>
      ),
    },
    {
      title: 'Действие',
      key: 'action',
      width: 160,
      render: (_, record) => (
        <Space size="small">
          <Button
            size="small"
            type="primary"
            ghost
            onClick={() => navigate(`/briefs/${record.brief_id}/review`)}
          >
            Требования
          </Button>
          <Button
            size="small"
            onClick={() => navigate(`/runs/config/${record.brief_id}`)}
          >
            Расчёт
          </Button>
        </Space>
      ),
    },
  ];

  return (
    <Space direction="vertical" size="large" style={{ width: '100%' }}>
      <div>
        <Title level={2} style={{ margin: 0, color: '#f8fafc' }}>
          Технические задания
        </Title>
        <Text style={{ color: '#94a3b8', fontSize: 13 }}>
          Загрузка и структурирование требований заказчика в роли, компетенции и трудоемкость
        </Text>
      </div>

      <Row gutter={[24, 24]}>
        <Col xs={24} lg={14}>
          <Card
            title={
              <Space align="center">
                <FileTextOutlined style={{ color: '#3b82f6' }} />
                <span>Новое техническое задание</span>
              </Space>
            }
            extra={
              <Button
                size="small"
                icon={<ExperimentOutlined />}
                onClick={handleLoadSample}
                style={{ color: '#60a5fa', borderColor: 'rgba(59, 130, 246, 0.3)' }}
              >
                Загрузить образец ТЗ
              </Button>
            }
          >
            <Form form={form} layout="vertical" onFinish={submit}>
              <Form.Item
                name="title"
                label={<Text strong>Название проекта / ТЗ</Text>}
                rules={[{ required: true, message: 'Укажите название ТЗ' }]}
              >
                <Input placeholder="Например: Перевод личного кабинета на микросервисы" />
              </Form.Item>

              <Form.Item label={<Text strong>Загрузка через файл (.txt, .docx, .pdf, .md, .json)</Text>}>
                {uploadedFile ? (
                  <div
                    style={{
                      padding: '12px 16px',
                      background: 'rgba(59, 130, 246, 0.08)',
                      border: '1px solid rgba(59, 130, 246, 0.3)',
                      borderRadius: 8,
                      display: 'flex',
                      justifyContent: 'space-between',
                      alignItems: 'center',
                    }}
                  >
                    <Space align="center">
                      <FileDoneOutlined style={{ color: '#3b82f6', fontSize: 20 }} />
                      <div>
                        <Text strong style={{ color: '#f8fafc' }}>{uploadedFile.name}</Text>
                        <div>
                          <Text style={{ fontSize: 12, color: '#94a3b8' }}>
                            {(uploadedFile.size / 1024).toFixed(1)} КБ • {uploadedFile.type || 'документ'}
                          </Text>
                        </div>
                      </div>
                    </Space>
                    <Button
                      type="text"
                      danger
                      icon={<CloseCircleOutlined />}
                      onClick={handleClearFile}
                    >
                      Удалить
                    </Button>
                  </div>
                ) : (
                  <Upload.Dragger
                    accept=".txt,.md,.json,.docx,.pdf"
                    maxCount={1}
                    beforeUpload={handleFileDrop}
                    showUploadList={false}
                  >
                    <p className="ant-upload-drag-icon">
                      <InboxOutlined style={{ fontSize: 32, color: '#3b82f6' }} />
                    </p>
                    <p className="ant-upload-text" style={{ fontSize: 14, fontWeight: 500, color: '#f8fafc' }}>
                      Перетащите файл ТЗ сюда или нажмите для выбора
                    </p>
                    <p className="ant-upload-hint" style={{ color: '#94a3b8', fontSize: 12 }}>
                      Поддерживаются документы до 10 МБ с автоматическим распознаванием текста
                    </p>
                  </Upload.Dragger>
                )}
              </Form.Item>

              <Form.Item
                name="text"
                label={<Text strong>Текст требований (ТЗ)</Text>}
                rules={[{ required: !uploadedFile, message: 'Введите текст ТЗ или загрузите документ' }]}
              >
                <Input.TextArea
                  rows={7}
                  placeholder="Вставьте исходный текст технического задания заказчика..."
                  style={{ fontFamily: 'inherit' }}
                />
              </Form.Item>

              <Button
                type="primary"
                htmlType="submit"
                loading={submitting}
                icon={<SendOutlined />}
                style={{ background: '#3b82f6', borderColor: '#3b82f6', fontWeight: 500, height: 40 }}
                block
              >
                Извлечь требования (LLM / Rules)
              </Button>
            </Form>
          </Card>
        </Col>

        <Col xs={24} lg={10}>
          <Card title="Инструкция по загрузке ТЗ" style={{ height: '100%' }}>
            <Space direction="vertical" size="middle" style={{ width: '100%' }}>
              <Alert
                type="info"
                showIcon
                message="Принцип «Solver proposes, Go verifies»"
                description="LLM извлекает роли, компетенции, грейды и трудоемкость. Деперсонализация текста производится автоматически перед обращением к модели."
              />
              <div>
                <Text strong style={{ color: '#f8fafc' }}>Поддерживаемые форматы файлов:</Text>
                <ul style={{ paddingLeft: 20, color: '#94a3b8', fontSize: 13, marginTop: 6 }}>
                  <li><b>.txt / .md</b>: мгновенное чтение в форму с подсчетом символов</li>
                  <li><b>.docx</b>: распаковка OpenXML с выделением параграфов</li>
                  <li><b>.pdf</b>: текстовое извлечение структуры ТЗ</li>
                  <li><b>.json</b>: структурированные требования</li>
                </ul>
              </div>
              <div>
                <Text strong style={{ color: '#f8fafc' }}>Этапы пайплайна:</Text>
                <ol style={{ paddingLeft: 20, color: '#94a3b8', fontSize: 13, marginTop: 6 }}>
                  <li>Загрузка ТЗ → извлечение требований (1-3 сек)</li>
                  <li>Аудит и подтверждение требований в таблице</li>
                  <li>Формирование 2-3 вариантов команды в CP-SAT v2</li>
                </ol>
              </div>
            </Space>
          </Card>
        </Col>
      </Row>

      <Card
        title={
          <Space align="center">
            <span>Реестр загруженных технических заданий</span>
            <Badge count={briefs.length} style={{ backgroundColor: 'rgba(59, 130, 246, 0.2)', color: '#60a5fa' }} />
          </Space>
        }
      >
        <Table
          dataSource={briefs}
          columns={columns}
          rowKey="brief_id"
          loading={loading}
          pagination={{ pageSize: 8 }}
          locale={{ emptyText: 'Пока нет загруженных ТЗ. Загрузите файл выше или нажмите «Загрузить образец ТЗ».' }}
        />
      </Card>
    </Space>
  );
}

function TooltipWrapper({ title, children }: { title: string; children: ReactNode }) {
  return <span title={title}>{children}</span>;
}
