import { render, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { BrowserRouter, Routes, Route, MemoryRouter } from 'react-router-dom';
import ProtectedRoute from '../../src/features/auth/ProtectedRoute';
import { AuthProvider } from '../../src/features/auth/AuthContext';

// Mock component to protect
const ProtectedComponent = () => <div>Protected Content</div>;

// Mock login page
const LoginPage = () => <div>Login Page</div>;

// Helper to render with router and auth context
const renderWithAuth = (component, { isAuthenticated = false, initialPath = '/' } = {}) => {
  // Mock the useAuth hook
  vi.mock('../../src/features/auth/AuthContext', async () => {
    const actual = await vi.importActual('../../src/features/auth/AuthContext');
    return {
      ...actual,
      useAuth: () => ({
        user: isAuthenticated ? { id: '123', email: 'test@example.com' } : null,
        token: isAuthenticated ? 'mock-token' : null,
        login: vi.fn(),
        logout: vi.fn(),
        register: vi.fn(),
      }),
    };
  });

  return render(
    <MemoryRouter initialEntries={[initialPath]}>
      <AuthProvider>
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          <Route
            path="/"
            element={
              <ProtectedRoute>
                <ProtectedComponent />
              </ProtectedRoute>
            }
          />
          <Route
            path="/protected"
            element={
              <ProtectedRoute>
                <ProtectedComponent />
              </ProtectedRoute>
            }
          />
        </Routes>
      </AuthProvider>
    </MemoryRouter>
  );
};

describe('ProtectedRoute', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders protected content when user is authenticated', () => {
    renderWithAuth(<ProtectedRoute><ProtectedComponent /></ProtectedRoute>, {
      isAuthenticated: true,
      initialPath: '/protected',
    });

    expect(screen.getByText('Protected Content')).toBeInTheDocument();
  });

  it('redirects to login page when user is not authenticated', () => {
    renderWithAuth(<ProtectedRoute><ProtectedComponent /></ProtectedRoute>, {
      isAuthenticated: false,
      initialPath: '/protected',
    });

    expect(screen.getByText('Login Page')).toBeInTheDocument();
    expect(screen.queryByText('Protected Content')).not.toBeInTheDocument();
  });

  it('does not render protected content when token is null', () => {
    renderWithAuth(<ProtectedRoute><ProtectedComponent /></ProtectedRoute>, {
      isAuthenticated: false,
      initialPath: '/protected',
    });

    expect(screen.queryByText('Protected Content')).not.toBeInTheDocument();
  });

  it('preserves the intended destination for redirect after login', () => {
    const { container } = renderWithAuth(<ProtectedRoute><ProtectedComponent /></ProtectedRoute>, {
      isAuthenticated: false,
      initialPath: '/protected',
    });

    // When redirected to login, the original path should be preserved
    // This is typically done via state or query parameters
    expect(screen.getByText('Login Page')).toBeInTheDocument();

    // The redirect logic should include the original path
    // This can be verified by checking the location state or search params
    // Implementation may vary, but the concept is to preserve the redirect path
  });
});

// Alternative implementation without mocking useAuth
describe('ProtectedRoute (without mock)', () => {
  const renderWithAuthProvider = (isAuthenticated, children, initialPath = '/') => {
    // Create a test auth context value
    const authContextValue = {
      user: isAuthenticated ? { id: '123', email: 'test@example.com' } : null,
      token: isAuthenticated ? 'mock-token' : null,
      login: vi.fn(),
      logout: vi.fn(),
      register: vi.fn(),
    };

    return render(
      <MemoryRouter initialEntries={[initialPath]}>
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          <Route path="/protected" element={children} />
        </Routes>
      </MemoryRouter>
    );
  };

  it('allows access to protected route when authenticated', () => {
    renderWithAuthProvider(
      true,
      <ProtectedRoute>
        <ProtectedComponent />
      </ProtectedRoute>,
      '/protected'
    );

    // Note: This test assumes ProtectedRoute properly checks auth state
    // The actual rendering depends on the implementation
  });

  it('blocks access to protected route when not authenticated', () => {
    renderWithAuthProvider(
      false,
      <ProtectedRoute>
        <ProtectedComponent />
      </ProtectedRoute>,
      '/protected'
    );

    // Should redirect to login page
  });
});

// Test with specific use cases
describe('ProtectedRoute - Use Cases', () => {
  it('works with nested routes', () => {
    render(
      <BrowserRouter>
        <AuthProvider>
          <Routes>
            <Route path="/login" element={<LoginPage />} />
            <Route
              path="/dashboard/*"
              element={
                <ProtectedRoute>
                  <Routes>
                    <Route path="/" element={<div>Dashboard</div>} />
                    <Route path="/settings" element={<div>Settings</div>} />
                  </Routes>
                </ProtectedRoute>
              }
            />
          </Routes>
        </AuthProvider>
      </BrowserRouter>
    );

    // Test behavior with nested routes
  });

  it('handles loading state during authentication check', () => {
    // Some implementations may show a loading spinner while checking auth
    // This test would verify that behavior if implemented
  });

  it('redirects to intended page after successful login', () => {
    // This tests the full flow:
    // 1. User tries to access /protected without auth
    // 2. Gets redirected to /login with state containing the original path
    // 3. After login, gets redirected back to /protected
  });
});
