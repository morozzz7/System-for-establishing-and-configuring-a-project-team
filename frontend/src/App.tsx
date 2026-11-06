import { Button, Layout, Typography, Space, Card } from 'antd';
import {
  TeamOutlined,
  RocketOutlined,
} from '@ant-design/icons';

const { Header, Content, Footer } = Layout;
const { Title, Paragraph } = Typography;

/**
 * Landing page for the MVP.
 * UI language is Russian (i18n-ready for future locale switch).
 */
function App() {
  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Header
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 12,
          background: '#001529',
        }}
      >
        <TeamOutlined style={{ fontSize: 24, color: '#fff' }} />
        <Title level={4} style={{ color: '#fff', margin: 0 }}>
          Формирование проектной команды
        </Title>
      </Header>

      <Content style={{ padding: '48px 24px' }}>
        <div style={{ maxWidth: 800, margin: '0 auto' }}>
          <Card>
            <Space direction="vertical" size="large" style={{ width: '100%' }}>
              <Title level={2}>
                Система формирования проектной команды (MVP)
              </Title>

              <Paragraph>
                Система поддержки принятия решений для ресурсных менеджеров.
                По техническому заданию подбирает оптимальную проектную команду
                из доступных сотрудников с учётом компетенций, загрузки,
                отпусков и дефицитов.
              </Paragraph>

              <Paragraph type="secondary">
                Фаза 1.3 — инфраструктурный каркас. Бизнес-эндпоинты будут
                добавлены в фазе 2.x.
              </Paragraph>

              <Space>
                <Button
                  type="primary"
                  icon={<RocketOutlined />}
                  size="large"
                  onClick={() => {
                    // Placeholder: brief intake will be implemented in 2.x.
                    alert('Подбор команды будет доступен в фазе 2.x');
                  }}
                >
                  Подобрать команду
                </Button>
              </Space>
            </Space>
          </Card>
        </div>
      </Content>

      <Footer style={{ textAlign: 'center' }}>
        Система формирования проектной команды © 2026
      </Footer>
    </Layout>
  );
}

export default App;
