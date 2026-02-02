import React from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { useAuth } from '../../features/auth/AuthContext';
import './Header.css';

/**
 * Header component - Top navigation bar
 * Shows app logo, navigation links, and user menu
 */
const Header: React.FC = () => {
  const { user, logout } = useAuth();
  const navigate = useNavigate();

  const handleLogout = () => {
    logout();
    navigate('/login');
  };

  return (
    <header className="header">
      <div className="header-container">
        <Link to="/" className="logo">
          <span className="logo-icon">🌙</span>
          <span className="logo-text">MoonTrack</span>
        </Link>

        {user && (
          <>
            <nav className="main-nav">
              <Link to="/" className="nav-link">
                Dashboard
              </Link>
              <Link to="/wallets" className="nav-link">
                Wallets
              </Link>
              <Link to="/transactions" className="nav-link">
                Transactions
              </Link>
            </nav>

            <div className="user-menu">
              <span className="user-email">{user.email}</span>
              <button onClick={handleLogout} className="btn-logout">
                Logout
              </button>
            </div>
          </>
        )}
      </div>
    </header>
  );
};

export default Header;
