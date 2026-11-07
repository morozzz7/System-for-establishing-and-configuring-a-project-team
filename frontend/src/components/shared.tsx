/**
 * Shared UI components.
 */
import { Progress, Typography, Space, Tag } from 'antd';
import type { Grade, Level } from '../types';

const { Text } = Typography;

// ──── LevelBar ────

/** Competency level bar (1-5) with color coding. */
export function LevelBar({ level, confidence }: { level: Level; confidence?: number }) {
  const percent = (level / 5) * 100;
  const color =
    level >= 4 ? '#52c41a' : level >= 3 ? '#1890ff' : level >= 2 ? '#faad14' : '#ff4d4f';
  return (
    <Space>
      <Progress
        percent={percent}
        size="small"
        strokeColor={color}
        showInfo={false}
        style={{ width: 100 }}
      />
      <Text>
        {level}/5
        {confidence !== undefined && (
          <Text type="secondary" style={{ marginLeft: 4, fontSize: 12 }}>
            (conf: {Math.round(confidence * 100)}%)
          </Text>
        )}
      </Text>
    </Space>
  );
}

// ──── GradeTag ────

const GRADE_COLORS: Record<Grade, string> = {
  J: 'green',
  M: 'blue',
  S: 'purple',
  L: 'orange',
  P: 'red',
};

export function GradeTag({ grade }: { grade: Grade }) {
  return <Tag color={GRADE_COLORS[grade]}>{grade}</Tag>;
}

// ──── StatusTag ────

const STATUS_COLORS: Record<string, string> = {
  planned: 'default',
  active: 'processing',
  paused: 'warning',
  done: 'success',
  cancelled: 'default',
  vacation: 'blue',
  sick_leave: 'red',
  training: 'purple',
  business_trip: 'cyan',
  parental_leave: 'green',
  other: 'default',
};

export function StatusTag({ status }: { status: string }) {
  return <Tag color={STATUS_COLORS[status] ?? 'default'}>{status}</Tag>;
}

// ──── Loading / Empty / Error helpers ────

export function Loading() {
  return <Text type="secondary">Загрузка...</Text>;
}

export function Empty({ message = 'Нет данных' }: { message?: string }) {
  return <Text type="secondary">{message}</Text>;
}

export function ErrorDisplay({ error }: { error: string }) {
  return <Text type="danger">Ошибка: {error}</Text>;
}
