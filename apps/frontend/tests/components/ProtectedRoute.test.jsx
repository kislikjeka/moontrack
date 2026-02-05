import { render, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { MemoryRouter, Routes, Route } from 'react-router-dom';

// Mock at module level
vi.mock('../../src/features/auth/useAuth', () => ({
  useAuth: vi.fn(),
}));

import ProtectedRoute from '../../src/features/auth/ProtectedRoute';
import { useAuth } from '../../src/features/auth/useAuth';

const ProtectedComponent = () => <div>Protected Content</div>;
const LoginPage = () => <div>Login Page</div>;

const renderWithRouter = (initialPath = '/') => {
  return render(
    <MemoryRouter initialEntries={[initialPath]}>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route path="/protected" element={<ProtectedRoute><ProtectedComponent /></ProtectedRoute>} />
      </Routes>
    </MemoryRouter>
  );
};

describe('ProtectedRoute', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders protected content when authenticated', () => {
    useAuth.mockReturnValue({ isAuthenticated: () => true, loading: false });
    renderWithRouter('/protected');
    expect(screen.getByText('Protected Content')).toBeInTheDocument();
  });

  it('redirects to login when not authenticated', () => {
    useAuth.mockReturnValue({ isAuthenticated: () => false, loading: false });
    renderWithRouter('/protected');
    expect(screen.getByText('Login Page')).toBeInTheDocument();
  });

  it('shows loading state', () => {
    useAuth.mockReturnValue({ isAuthenticated: () => false, loading: true });
    renderWithRouter('/protected');
    expect(screen.queryByText('Protected Content')).not.toBeInTheDocument();
    expect(screen.queryByText('Login Page')).not.toBeInTheDocument();
  });
});
