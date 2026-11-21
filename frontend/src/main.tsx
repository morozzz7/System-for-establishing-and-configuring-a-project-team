import React from 'react';
import ReactDOM from 'react-dom/client';
import { ConfigProvider, theme } from 'antd';
import ruRU from 'antd/locale/ru_RU';
import App from './App';
import './index.css';

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <ConfigProvider
      locale={ruRU}
      theme={{
        algorithm: theme.darkAlgorithm,
        token: {
          colorPrimary: '#3b82f6',
          colorInfo: '#3b82f6',
          colorSuccess: '#10b981',
          colorWarning: '#f59e0b',
          colorError: '#ef4444',
          colorBgBase: '#090d16',
          colorBgContainer: '#111827',
          colorBgElevated: '#162032',
          colorBgLayout: '#090d16',
          colorBorder: 'rgba(255, 255, 255, 0.08)',
          colorBorderSecondary: 'rgba(255, 255, 255, 0.05)',
          colorText: '#f8fafc',
          colorTextSecondary: '#94a3b8',
          colorTextTertiary: '#64748b',
          borderRadius: 10,
          fontFamily: '-apple-system, BlinkMacSystemFont, "Geist", "Inter", "Segoe UI", Roboto, sans-serif',
          fontSize: 14,
        },
        components: {
          Layout: {
            headerBg: '#0b101c',
            bodyBg: '#090d16',
            siderBg: '#0b101c',
          },
          Card: {
            headerBg: 'transparent',
            colorBorderSecondary: 'rgba(255, 255, 255, 0.08)',
          },
          Table: {
            headerBg: '#0d1424',
            rowHoverBg: 'rgba(59, 130, 246, 0.06)',
            borderColor: 'rgba(255, 255, 255, 0.05)',
          },
          Button: {
            borderRadius: 8,
            controlHeight: 36,
          },
          Modal: {
            contentBg: '#111827',
            headerBg: '#111827',
          },
          Drawer: {
            colorBgElevated: '#0f172a',
          },
          Menu: {
            darkItemBg: '#0b101c',
            darkSubMenuItemBg: '#080c16',
            darkItemSelectedBg: 'rgba(59, 130, 246, 0.15)',
            darkItemSelectedColor: '#60a5fa',
          },
        },
      }}
    >
      <App />
    </ConfigProvider>
  </React.StrictMode>,
);
