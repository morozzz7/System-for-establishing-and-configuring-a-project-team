/**
 * Employee bulk import modal component.
 * Supports drag-and-drop JSON/CSV, client/server parsing, live preview table,
 * dry-run validation (validate_only), error row highlights, and CSV template download.
 */
import { useState } from 'react';
import {
  Modal,
  Upload,
  Button,
  Space,
  Typography,
  Table,
  Tag,
  Alert,
  Segmented,
  message,
  Tooltip,
} from 'antd';
import {
  InboxOutlined,
  DownloadOutlined,
  CheckCircleOutlined,
  ExclamationCircleOutlined,
  SafetyCertificateOutlined,
  UploadOutlined,
  ReloadOutlined,
} from '@ant-design/icons';
import type { ColumnsType } from 'antd/es/table';
import { importEmployees } from '../../api/client';
import type { EmployeeImportPayload, EmployeeImportResponse } from '../../api/client';
import { GradeTag } from '../shared';
import { mockRoles } from '../../mocks/reference';
import type { Grade } from '../../types';
import {
  downloadCsvTemplate,
  parseCsvToEmployees,
  parseJsonToEmployees,
} from '../../utils/csvImport';
import type { ParsedEmployeeRow } from '../../utils/csvImport';

const { Text } = Typography;

export interface EmployeeImportModalProps {
  open: boolean;
  onClose: () => void;
  onSuccess: () => void;
}

export default function EmployeeImportModal({ open, onClose, onSuccess }: EmployeeImportModalProps) {
  const [rows, setRows] = useState<ParsedEmployeeRow[]>([]);
  const [fileName, setFileName] = useState<string>('');
  const [filterMode, setFilterMode] = useState<string>('all');
  const [importMode] = useState<'upsert' | 'replace'>('upsert');
  const [dryRunResult, setDryRunResult] = useState<EmployeeImportResponse | null>(null);
  const [validating, setValidating] = useState<boolean>(false);
  const [submitting, setSubmitting] = useState<boolean>(false);

  const resetState = () => {
    setRows([]);
    setFileName('');
    setFilterMode('all');
    setDryRunResult(null);
    setValidating(false);
    setSubmitting(false);
  };

  const handleClose = () => {
    resetState();
    onClose();
  };

  const handleFile = async (file: File) => {
    try {
      const text = await file.text();
      let parsed: ParsedEmployeeRow[] = [];
      if (file.name.endsWith('.csv') || (text.includes(',') && !text.trim().startsWith('{') && !text.trim().startsWith('['))) {
        parsed = parseCsvToEmployees(text);
      } else {
        const json = JSON.parse(text);
        parsed = parseJsonToEmployees(json);
      }

      if (parsed.length === 0) {
        message.error('Файл пуст или имеет неподдерживаемый формат');
        return false;
      }

      setFileName(file.name);
      setRows(parsed);
      setDryRunResult(null);
      message.success(`Загружено записей: ${parsed.length}`);
    } catch (err) {
      message.error(`Ошибка чтения файла: ${err instanceof Error ? err.message : String(err)}`);
    }
    return false;
  };

  const handleDryRun = async () => {
    if (rows.length === 0) return;
    setValidating(true);
    try {
      const payload: EmployeeImportPayload = {
        mode: importMode,
        validate_only: true,
        employees: rows.map((r) => r.raw),
      };
      const res = await importEmployees(payload);
      setDryRunResult(res);

      if (res.validation_errors && res.validation_errors.length > 0) {
        setRows((prev) =>
          prev.map((r) => {
            const err = res.validation_errors.find((ve) => ve.employee_id === r.employee_id);
            if (err) {
              return { ...r, has_error: true, error_message: `${err.field}: ${err.detail}` };
            }
            return r;
          }),
        );
        message.warning(`Обнаружено ошибок: ${res.validation_errors.length}`);
      } else {
        message.success('Проверка (Dry Run) успешно пройдена без ошибок!');
      }
    } catch (err) {
      message.error(`Ошибка валидации: ${err instanceof Error ? err.message : String(err)}`);
    } finally {
      setValidating(false);
    }
  };

  const handleConfirmImport = async () => {
    const validRows = rows.filter((r) => !r.has_error);
    if (validRows.length === 0) {
      message.error('Нет корректных строк для импорта');
      return;
    }

    setSubmitting(true);
    try {
      const payload: EmployeeImportPayload = {
        mode: importMode,
        validate_only: false,
        employees: validRows.map((r) => r.raw),
      };
      const res = await importEmployees(payload);
      message.success(
        `Импорт завершен: создано ${res.totals.created}, обновлено ${res.totals.updated}, ошибок ${res.totals.errors}`,
      );
      handleClose();
      onSuccess();
    } catch (err) {
      message.error(`Ошибка импорта: ${err instanceof Error ? err.message : String(err)}`);
    } finally {
      setSubmitting(false);
    }
  };

  const errorCount = rows.filter((r) => r.has_error).length;
  const validCount = rows.length - errorCount;

  const displayRows = rows.filter((r) => {
    if (filterMode === 'valid') return !r.has_error;
    if (filterMode === 'error') return r.has_error;
    return true;
  });

  const columns: ColumnsType<ParsedEmployeeRow> = [
    {
      title: '#',
      dataIndex: 'index',
      key: 'index',
      width: 50,
      render: (idx: number) => <Text style={{ color: '#64748b' }}>{idx}</Text>,
    },
    {
      title: 'ID',
      dataIndex: 'employee_id',
      key: 'employee_id',
      render: (id: string, record: ParsedEmployeeRow) => (
        <Space direction="horizontal" size="small">
          <Text strong style={{ color: record.has_error ? '#ef4444' : '#f8fafc' }}>
            {id || '—'}
          </Text>
        </Space>
      ),
    },
    {
      title: 'Грейд',
      dataIndex: 'grade',
      key: 'grade',
      width: 80,
      render: (grade: string) => <GradeTag grade={grade as Grade} />,
    },
    {
      title: 'Роль',
      dataIndex: 'role_profile_id',
      key: 'role_profile_id',
      render: (roleId: string) => {
        const role = mockRoles.find((r) => r.role_id === roleId);
        return <Text style={{ color: '#94a3b8' }}>{role ? role.name_ru : roleId || '—'}</Text>;
      },
    },
    {
      title: 'Занятость',
      dataIndex: 'employment_type',
      key: 'employment_type',
      width: 110,
      render: (type: string) => <Text style={{ fontSize: 12 }}>{type === 'full_time' ? '100% (FT)' : '50% (PT)'}</Text>,
    },
    {
      title: 'Скамейка',
      dataIndex: 'bench_flag',
      key: 'bench_flag',
      width: 90,
      render: (bench: boolean) => (
        <Tag color={bench ? 'green' : 'default'} style={{ fontSize: 11 }}>
          {bench ? 'Бенч' : 'Проект'}
        </Tag>
      ),
    },
    {
      title: 'Компетенции',
      dataIndex: 'competencies_count',
      key: 'competencies_count',
      width: 110,
      render: (count: number) => <Tag color="blue">{count} навыков</Tag>,
    },
    {
      title: 'Статус',
      key: 'status',
      render: (_: unknown, record: ParsedEmployeeRow) =>
        record.has_error ? (
          <Tooltip title={record.error_message}>
            <Tag icon={<ExclamationCircleOutlined />} color="error">
              {record.error_message || 'Ошибка'}
            </Tag>
          </Tooltip>
        ) : (
          <Tag icon={<CheckCircleOutlined />} color="success">
            Готов
          </Tag>
        ),
    },
  ];

  return (
    <Modal
      title={
        <Space direction="horizontal" size="middle">
          <UploadOutlined style={{ color: '#3b82f6', fontSize: 18 }} />
          <span>Массовая загрузка сотрудников (JSON / CSV)</span>
        </Space>
      }
      open={open}
      onCancel={handleClose}
      width={920}
      footer={
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
          <Space>
            <Button icon={<DownloadOutlined />} onClick={downloadCsvTemplate}>
              Шаблон CSV
            </Button>
            {rows.length > 0 && (
              <Button
                icon={<SafetyCertificateOutlined />}
                onClick={handleDryRun}
                loading={validating}
                style={{ borderColor: '#3b82f6', color: '#60a5fa' }}
              >
                Проверить (Dry Run)
              </Button>
            )}
          </Space>
          <Space>
            <Button onClick={handleClose}>Отмена</Button>
            <Button
              type="primary"
              onClick={handleConfirmImport}
              loading={submitting}
              disabled={validCount === 0}
              style={{ background: '#3b82f6', borderColor: '#3b82f6' }}
            >
              Импортировать ({validCount})
            </Button>
          </Space>
        </div>
      }
    >
      <Space direction="vertical" size="middle" style={{ width: '100%' }}>
        {rows.length === 0 ? (
          <div>
            <Upload.Dragger
              accept=".json,.csv,text/csv,application/json"
              maxCount={1}
              beforeUpload={handleFile}
              showUploadList={false}
              className="humb-dropzone"
            >
              <p className="ant-upload-drag-icon">
                <InboxOutlined style={{ fontSize: 36, color: '#3b82f6' }} />
              </p>
              <p className="ant-upload-text" style={{ fontSize: 15, fontWeight: 500, color: '#f8fafc' }}>
                Перетащите JSON или CSV файл сюда или нажмите для выбора
              </p>
              <p className="ant-upload-hint" style={{ color: '#94a3b8', fontSize: 13 }}>
                Поддерживаются схемы EmployeeImportRequest (OpenAPI) и табличный CSV с автоматическим распознаванием колонок.
              </p>
            </Upload.Dragger>
            <div style={{ marginTop: 16, display: 'flex', justifyContent: 'flex-end' }}>
              <Button type="link" icon={<DownloadOutlined />} onClick={downloadCsvTemplate} style={{ color: '#60a5fa' }}>
                Скачать готовый образец CSV-шаблона
              </Button>
            </div>
          </div>
        ) : (
          <div>
            <div
              style={{
                display: 'flex',
                justifyContent: 'space-between',
                alignItems: 'center',
                padding: '12px 16px',
                background: 'rgba(255, 255, 255, 0.03)',
                borderRadius: 8,
                border: '1px solid rgba(255, 255, 255, 0.06)',
              }}
            >
              <Space direction="horizontal" size="middle">
                <Text strong style={{ color: '#f8fafc' }}>
                  Файл: {fileName}
                </Text>
                <Segmented
                  value={filterMode}
                  onChange={(val) => setFilterMode(String(val))}
                  options={[
                    { label: `Все (${rows.length})`, value: 'all' },
                    { label: `Готовы (${validCount})`, value: 'valid' },
                    { label: `Ошибки (${errorCount})`, value: 'error' },
                  ]}
                />
              </Space>
              <Space>
                <Button size="small" icon={<ReloadOutlined />} onClick={() => setRows([])}>
                  Выбрать другой файл
                </Button>
              </Space>
            </div>

            {dryRunResult && (
              <Alert
                style={{ marginTop: 12 }}
                type={dryRunResult.totals.errors === 0 ? 'success' : 'warning'}
                showIcon
                message={
                  dryRunResult.totals.errors === 0
                    ? `Dry Run успешен: ${dryRunResult.totals.received} записей готовы к импорту.`
                    : `Dry Run выявил ${dryRunResult.totals.errors} ошибок валидации.`
                }
                description={
                  <Space size="large" style={{ marginTop: 4 }}>
                    <Text>Получено: {dryRunResult.totals.received}</Text>
                    <Text>К созданию: {dryRunResult.totals.created}</Text>
                    <Text>К обновлению: {dryRunResult.totals.updated}</Text>
                    <Text type="danger">Ошибок: {dryRunResult.totals.errors}</Text>
                  </Space>
                }
              />
            )}

            <Table
              style={{ marginTop: 12 }}
              dataSource={displayRows}
              columns={columns}
              rowKey="key"
              size="small"
              pagination={{ pageSize: 6 }}
              rowClassName={(record) => (record.has_error ? 'table-row-error' : '')}
            />
          </div>
        )}
      </Space>
    </Modal>
  );
}
