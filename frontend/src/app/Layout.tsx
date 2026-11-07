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
} from '@ant-design/icons';
import { Outlet, useNavigate, useLocation } from 'react-router-dom';

const { Header, Sider, Content } = Layout;
const { Title } = Typography;

const menuItems = [
  { key: '/', icon: <DashboardOutlined />, label: 'Дашборд' },
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
          defaultOpenKeys={['reference']}
          items={menuItems}
          onClick={({ key }) => navigate(key)}
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
