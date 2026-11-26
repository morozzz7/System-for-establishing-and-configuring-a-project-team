/**
 * Main application layout with side navigation in Humblytics dark slate aesthetic.
 */
import { Layout, Menu, Typography, Space, Button, Badge } from 'antd';
import {
  DashboardOutlined,
  TeamOutlined,
  ProjectOutlined,
  ScheduleOutlined,
  CalendarOutlined,
  BookOutlined,
  FileTextOutlined,
  SettingOutlined,
  PlusOutlined,
  CloudUploadOutlined,
  ThunderboltFilled,
} from '@ant-design/icons';
import { useState } from 'react';
import { Outlet, useNavigate, useLocation } from 'react-router-dom';

const { Header, Sider, Content } = Layout;
const { Title, Text } = Typography;

/**
 * Leaf menu keys that map 1:1 to registered routes (see `src/App.tsx`).
 */
const MENU_LEAF_KEYS: ReadonlySet<string> = new Set([
  '/',
  '/briefs',
  '/runs/config',
  '/employees',
  '/projects',
  '/assignments',
  '/absences',
  '/reference/roles',
  '/reference/competencies',
  '/reference/grades',
  '/reference/weight-presets',
  '/reference/planning-params',
]);

function selectedMenuKey(pathname: string): string {
  if (MENU_LEAF_KEYS.has(pathname)) return pathname;
  return [...MENU_LEAF_KEYS].find((key) => key !== '/' && pathname.startsWith(`${key}/`)) ?? pathname;
}

const menuItems = [
  { key: '/', icon: <DashboardOutlined />, label: 'Дашборд' },
  { key: '/briefs', icon: <FileTextOutlined />, label: 'Технические задания' },
  { key: '/runs/config', icon: <SettingOutlined />, label: 'Настроить расчёт' },
  { key: '/employees', icon: <TeamOutlined />, label: 'Сотрудники' },
  { key: '/projects', icon: <ProjectOutlined />, label: 'Проекты' },
  { key: '/assignments', icon: <ScheduleOutlined />, label: 'Назначения' },
  { key: '/absences', icon: <CalendarOutlined />, label: 'Отсутствия' },
  {
    key: 'reference',
    icon: <BookOutlined />,
    label: 'Справочники',
    children: [
      { key: '/reference/roles', label: 'Роли' },
      { key: '/reference/competencies', label: 'Компетенции' },
      { key: '/reference/grades', label: 'Грейды' },
      { key: '/reference/weight-presets', label: 'Пресеты весов' },
      { key: '/reference/planning-params', label: 'Параметры планирования' },
    ],
  },
];

export default function AppLayout() {
  const navigate = useNavigate();
  const location = useLocation();
  const [openKeys, setOpenKeys] = useState<string[]>(['reference']);

  return (
    <Layout style={{ minHeight: '100vh', background: '#090d16' }}>
      <Sider
        breakpoint="lg"
        collapsedWidth={80}
        theme="dark"
        width={272}
        style={{
          background: '#0b101c',
          borderRight: '1px solid rgba(255, 255, 255, 0.08)',
        }}
      >
        <div style={{ padding: '20px 16px 14px', borderBottom: '1px solid rgba(255, 255, 255, 0.06)' }}>
          <Space direction="horizontal" align="center" style={{ width: '100%' }}>
            <div
              style={{
                width: 32,
                height: 32,
                borderRadius: 8,
                background: 'linear-gradient(135deg, #3b82f6 0%, #1d4ed8 100%)',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                boxShadow: '0 0 12px rgba(59, 130, 246, 0.4)',
              }}
            >
              <ThunderboltFilled style={{ fontSize: 18, color: '#fff' }} />
            </div>
            <div>
              <Title level={5} style={{ color: '#f8fafc', margin: 0, fontSize: 14, fontWeight: 600, letterSpacing: '-0.01em' }}>
                КомандаОпт
              </Title>
              <div style={{ display: 'flex', alignItems: 'center', marginTop: 2 }}>
                <span className="status-dot status-dot-green" />
                <Text style={{ color: '#94a3b8', fontSize: 11 }}>CP-SAT v2 • Онлайн</Text>
              </div>
            </div>
          </Space>
        </div>
        <div style={{ padding: '12px 8px' }}>
          <Menu
            theme="dark"
            mode="inline"
            selectedKeys={[selectedMenuKey(location.pathname)]}
            openKeys={openKeys}
            onOpenChange={(keys) => setOpenKeys(keys.map((key) => String(key)))}
            items={menuItems}
            style={{ background: 'transparent', borderRight: 0 }}
            onClick={({ key }) => {
              const normalized = key !== undefined ? String(key) : '';
              if (MENU_LEAF_KEYS.has(normalized)) {
                if (normalized.startsWith('/reference/')) {
                  setOpenKeys((prev) => (prev.includes('reference') ? prev : [...prev, 'reference']));
                }
                navigate(normalized);
                return;
              }
              if (normalized === 'reference') {
                setOpenKeys((prev) =>
                  prev.includes('reference') ? prev.filter((k) => k !== 'reference') : [...prev, 'reference'],
                );
              }
            }}
          />
        </div>
      </Sider>
      <Layout style={{ background: '#090d16' }}>
        <Header
          style={{
            background: '#0b101c',
            padding: '0 28px',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            borderBottom: '1px solid rgba(255, 255, 255, 0.08)',
            height: 64,
          }}
        >
          <Space align="center" size="middle">
            <Title level={4} style={{ margin: 0, color: '#f8fafc', fontSize: 16, fontWeight: 600 }}>
              Система формирования проектной команды
            </Title>
            <Badge
              count="2026-W47"
              style={{
                backgroundColor: 'rgba(59, 130, 246, 0.15)',
                color: '#60a5fa',
                borderColor: 'rgba(59, 130, 246, 0.3)',
                fontSize: 11,
              }}
            />
          </Space>
          <Space size="middle">
            <Button
              type="primary"
              icon={<PlusOutlined />}
              onClick={() => navigate('/briefs')}
              style={{
                background: '#3b82f6',
                borderColor: '#3b82f6',
                fontWeight: 500,
              }}
            >
              Новое ТЗ
            </Button>
            <Button
              icon={<CloudUploadOutlined />}
              onClick={() => navigate('/employees')}
              style={{
                background: 'rgba(255, 255, 255, 0.05)',
                borderColor: 'rgba(255, 255, 255, 0.12)',
                color: '#f8fafc',
              }}
            >
              Импорт пула
            </Button>
          </Space>
        </Header>
        <Content style={{ margin: 24, padding: 0, minHeight: 280 }}>
          <Outlet />
        </Content>
      </Layout>
    </Layout>
  );
}
