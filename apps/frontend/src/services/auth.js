import api from './api';

/**
 * Authentication service
 * Handles user registration, login, and logout
 */
const authService = {
  /**
   * Register a new user
   * @param {string} email - User's email
   * @param {string} password - User's password
   * @returns {Promise<{token: string, user: object}>}
   */
  async register(email, password) {
    const response = await api.post('/auth/register', {
      email,
      password,
    });

    const { token, user } = response.data;

    // Store token and user in localStorage
    if (token) {
      localStorage.setItem('auth_token', token);
      localStorage.setItem('user', JSON.stringify(user));
    }

    return response.data;
  },

  /**
   * Login user
   * @param {string} email - User's email
   * @param {string} password - User's password
   * @returns {Promise<{token: string, user: object}>}
   */
  async login(email, password) {
    const response = await api.post('/auth/login', {
      email,
      password,
    });

    const { token, user } = response.data;

    // Store token and user in localStorage
    if (token) {
      localStorage.setItem('auth_token', token);
      localStorage.setItem('user', JSON.stringify(user));
    }

    return response.data;
  },

  /**
   * Logout user
   */
  logout() {
    localStorage.removeItem('auth_token');
    localStorage.removeItem('user');
  },

  /**
   * Get current user from localStorage
   * @returns {object|null}
   */
  getCurrentUser() {
    const userStr = localStorage.getItem('user');
    if (userStr) {
      try {
        return JSON.parse(userStr);
      } catch (e) {
        return null;
      }
    }
    return null;
  },

  /**
   * Get auth token from localStorage
   * @returns {string|null}
   */
  getToken() {
    return localStorage.getItem('auth_token');
  },

  /**
   * Check if user is authenticated
   * @returns {boolean}
   */
  isAuthenticated() {
    return !!this.getToken();
  },
};

export default authService;
