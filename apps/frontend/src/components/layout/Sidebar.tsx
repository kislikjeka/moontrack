import React from 'react';
import { Link, useLocation } from 'react-router-dom';
import './Sidebar.css';

/**
 * Sidebar component - Side navigation menu
 * Shows main navigation links with active state
 */
const Sidebar: React.FC = () => {
  const location = useLocation();

  const isActive = (path: string) => {
    return location.pathname === path || location.pathname.startsWith(path + '/');
  };

  const menuItems = [
    { path: '/', label: 'Dashboard', icon: '📊' },
    { path: '/wallets', label: 'Wallets', icon: '💼' },
    { path: '/transactions', label: 'Transactions', icon: '📝' },
  ];

  return (
    <aside className="sidebar">
      <nav className="sidebar-nav">
        {menuItems.map((item) => (
          <Link
            key={item.path}
            to={item.path}
            className={`sidebar-link ${isActive(item.path) ? 'active' : ''}`}
          >
            <span className="sidebar-icon">{item.icon}</span>
            <span className="sidebar-label">{item.label}</span>
          </Link>
        ))}
      </nav>
    </aside>
  );
};

export default Sidebar;
