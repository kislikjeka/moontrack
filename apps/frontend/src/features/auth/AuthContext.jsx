import React, { createContext, useState, useContext, useEffect } from 'react';
import authService from '../../services/auth';

// Create the authentication context
const AuthContext = createContext(null);

/**
 * AuthProvider component
 * Provides authentication state and methods to the entire app
 */
export const AuthProvider = ({ children }) => {
  const [user, setUser] = useState(null);
  const [loading, setLoading] = useState(true);

  // Initialize user from localStorage on mount
  useEffect(() => {
    const currentUser = authService.getCurrentUser();
    if (currentUser) {
      setUser(currentUser);
    }
    setLoading(false);
  }, []);

  /**
   * Register a new user
   */
  const register = async (email, password) => {
    try {
      const data = await authService.register(email, password);
      setUser(data.user);
      return { success: true, data };
    } catch (error) {
      const message = error.response?.data?.error || 'Registration failed';
      return { success: false, error: message };
    }
  };

  /**
   * Login user
   */
  const login = async (email, password) => {
    try {
      const data = await authService.login(email, password);
      setUser(data.user);
      return { success: true, data };
    } catch (error) {
      const message = error.response?.data?.error || 'Login failed';
      return { success: false, error: message };
    }
  };

  /**
   * Logout user
   */
  const logout = () => {
    authService.logout();
    setUser(null);
  };

  /**
   * Check if user is authenticated
   */
  const isAuthenticated = () => {
    return !!user && authService.isAuthenticated();
  };

  const value = {
    user,
    loading,
    register,
    login,
    logout,
    isAuthenticated,
  };

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
};

/**
 * Custom hook to use authentication context
 * @returns {object} Authentication context value
 */
export const useAuth = () => {
  const context = useContext(AuthContext);
  if (!context) {
    throw new Error('useAuth must be used within an AuthProvider');
  }
  return context;
};

export default AuthContext;
