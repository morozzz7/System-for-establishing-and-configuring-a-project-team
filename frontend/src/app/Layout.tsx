/**
 * Main application layout with side navigation.
 */
import { Layout, Menu, Typography, Space } from 'antd';
import {
  DashboardOutlined,
  TeamOutlined,
  ProjectOutlined,
  ScheduleOutlined,
  CalendarOutlined,
  BookOutlined,
  FileTextOutlined,
  SettingOutlined,
} from '@ant-design/icons';
import { useState } from 'react';
import { Outlet, useNavigate, useLocation } from 'react-router-dom';

const { Header, Sider, Content } = Layout;
const { Title } = Typography;

/**
 * Leaf menu keys that map 1:1 to registered routes (see `src/App.tsx`).
 * The collapsed group key (`reference`) is intentionally absent: it only toggles the submenu.
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
    <Layout style={{ minHeight: '100vh' }}>
      <Sider breakpoint="lg" collapsedWidth={80} theme="dark">
        <div style={{ padding: '16px 16px 8px', textAlign: 'center' }}>
          <Space direction="vertical" size={0}>
            <TeamOutlined style={{ fontSize: 24, color: '#fff' }} />
            <Title level={5} style={{ color: '#fff', margin: 0, fontSize: 12 }}>
              Формирование команды
            </Title>
          </Space>
        </div>
        <Menu
          theme="dark"
          mode="inline"
          selectedKeys={[location.pathname]}
          openKeys={openKeys}
          onOpenChange={(keys) => setOpenKeys(keys.map((key) => String(key)))}
          items={menuItems}
          onClick={({ key }) => {
            const normalized = key !== undefined ? String(key) : '';
            // Only leaf routes are navigable; the reference group toggles its submenu.
            if (MENU_LEAF_KEYS.has(normalized)) {
              // Keep an expanded submenu open when a child route is chosen.
              if (normalized.startsWith('/reference/')) {
                setOpenKeys((prev) => (prev.includes('reference') ? prev : [...prev, 'reference']));
              }
              navigate(normalized);
              return;
            }
            if (normalized === 'reference') {
              setOpenKeys((prev) => (prev.includes('reference') ? prev.filter((k) => k !== 'reference') : [...prev, 'reference']));
            }
          }}
        />
      </Sider>
      <Layout>
        <Header
          style={{
            background: '#fff',
            padding: '0 24px',
            display: 'flex',
            alignItems: 'center',
            borderBottom: '1px solid #f0f0f0',
          }}
        >
          <Title level={4} style={{ margin: 0 }}>
            Система формирования проектной команды
          </Title>
        </Header>
        <Content style={{ margin: 24, padding: 24, background: '#fff', borderRadius: 8 }}>
          <Outlet />
        </Content>
      </Layout>
    </Layout>
  );
}
