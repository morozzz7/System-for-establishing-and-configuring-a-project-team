/**
 * In-app 404 screen (React Router catch-all).
 *
 * Used as the fallback route (`path="*"`) so that unknown deep links render a
 * friendly screen with a way back instead of leaving the content area blank.
 */
import { Button, Space, Typography } from 'antd';
import { useNavigate } from 'react-router-dom';

const { Title, Text } = Typography;

export default function NotFoundPage() {
  const navigate = useNavigate();

  return (
    <Space direction="vertical" size="large" style={{ width: '100%' }}>
      <Title level={2}>Страница не найдена</Title>
      <Text>
        Запрашиваемый раздел не существует или был перемещён. Проверьте адрес или вернитесь на дашборд.
      </Text>
      <Space>
        <Button type="primary" onClick={() => navigate('/')}>
          На дашборд
        </Button>
      </Space>
    </Space>
  );
}
