import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { BrowserRouter } from 'react-router-dom';
import RegistrationForm from '../../src/features/auth/RegistrationForm';
import { AuthProvider } from '../../src/features/auth/AuthContext';
import * as authService from '../../src/services/auth';

// Mock the auth service
vi.mock('../../src/services/auth');

// Wrapper component with necessary providers
const renderWithProviders = (component) => {
  return render(
    <BrowserRouter>
      <AuthProvider>
        {component}
      </AuthProvider>
    </BrowserRouter>
  );
};

describe('RegistrationForm', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders registration form with all fields', () => {
    renderWithProviders(<RegistrationForm />);

    expect(screen.getByLabelText(/email/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/password/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/confirm password/i)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /register|sign up/i })).toBeInTheDocument();
  });

  it('shows validation error for empty email', async () => {
    renderWithProviders(<RegistrationForm />);

    const submitButton = screen.getByRole('button', { name: /register|sign up/i });
    fireEvent.click(submitButton);

    await waitFor(() => {
      expect(screen.getByText(/email is required/i)).toBeInTheDocument();
    });
  });

  it('shows validation error for invalid email format', async () => {
    renderWithProviders(<RegistrationForm />);

    const emailInput = screen.getByLabelText(/email/i);
    fireEvent.change(emailInput, { target: { value: 'invalid-email' } });

    const submitButton = screen.getByRole('button', { name: /register|sign up/i });
    fireEvent.click(submitButton);

    await waitFor(() => {
      expect(screen.getByText(/invalid email|valid email/i)).toBeInTheDocument();
    });
  });

  it('shows validation error for short password', async () => {
    renderWithProviders(<RegistrationForm />);

    const emailInput = screen.getByLabelText(/email/i);
    const passwordInput = screen.getByLabelText(/^password/i);

    fireEvent.change(emailInput, { target: { value: 'test@example.com' } });
    fireEvent.change(passwordInput, { target: { value: 'short' } });

    const submitButton = screen.getByRole('button', { name: /register|sign up/i });
    fireEvent.click(submitButton);

    await waitFor(() => {
      expect(screen.getByText(/password.*8.*characters|at least 8/i)).toBeInTheDocument();
    });
  });

  it('shows validation error for non-matching passwords', async () => {
    renderWithProviders(<RegistrationForm />);

    const emailInput = screen.getByLabelText(/email/i);
    const passwordInput = screen.getByLabelText(/^password/i);
    const confirmPasswordInput = screen.getByLabelText(/confirm password/i);

    fireEvent.change(emailInput, { target: { value: 'test@example.com' } });
    fireEvent.change(passwordInput, { target: { value: 'SecureP@ssw0rd123' } });
    fireEvent.change(confirmPasswordInput, { target: { value: 'DifferentPassword' } });

    const submitButton = screen.getByRole('button', { name: /register|sign up/i });
    fireEvent.click(submitButton);

    await waitFor(() => {
      expect(screen.getByText(/passwords.*match|passwords do not match/i)).toBeInTheDocument();
    });
  });

  it('successfully registers user with valid inputs', async () => {
    const mockUser = {
      id: '123',
      email: 'test@example.com',
    };
    const mockToken = 'mock-jwt-token';

    authService.register.mockResolvedValue({
      user: mockUser,
      token: mockToken,
    });

    renderWithProviders(<RegistrationForm />);

    const emailInput = screen.getByLabelText(/email/i);
    const passwordInput = screen.getByLabelText(/^password/i);
    const confirmPasswordInput = screen.getByLabelText(/confirm password/i);

    fireEvent.change(emailInput, { target: { value: 'test@example.com' } });
    fireEvent.change(passwordInput, { target: { value: 'SecureP@ssw0rd123' } });
    fireEvent.change(confirmPasswordInput, { target: { value: 'SecureP@ssw0rd123' } });

    const submitButton = screen.getByRole('button', { name: /register|sign up/i });
    fireEvent.click(submitButton);

    await waitFor(() => {
      expect(authService.register).toHaveBeenCalledWith('test@example.com', 'SecureP@ssw0rd123');
    });
  });

  it('displays error message when registration fails', async () => {
    const errorMessage = 'Email already exists';
    authService.register.mockRejectedValue(new Error(errorMessage));

    renderWithProviders(<RegistrationForm />);

    const emailInput = screen.getByLabelText(/email/i);
    const passwordInput = screen.getByLabelText(/^password/i);
    const confirmPasswordInput = screen.getByLabelText(/confirm password/i);

    fireEvent.change(emailInput, { target: { value: 'existing@example.com' } });
    fireEvent.change(passwordInput, { target: { value: 'SecureP@ssw0rd123' } });
    fireEvent.change(confirmPasswordInput, { target: { value: 'SecureP@ssw0rd123' } });

    const submitButton = screen.getByRole('button', { name: /register|sign up/i });
    fireEvent.click(submitButton);

    await waitFor(() => {
      expect(screen.getByText(/email already exists/i)).toBeInTheDocument();
    });
  });

  it('disables submit button while registration is in progress', async () => {
    authService.register.mockImplementation(() => new Promise(resolve => setTimeout(resolve, 1000)));

    renderWithProviders(<RegistrationForm />);

    const emailInput = screen.getByLabelText(/email/i);
    const passwordInput = screen.getByLabelText(/^password/i);
    const confirmPasswordInput = screen.getByLabelText(/confirm password/i);

    fireEvent.change(emailInput, { target: { value: 'test@example.com' } });
    fireEvent.change(passwordInput, { target: { value: 'SecureP@ssw0rd123' } });
    fireEvent.change(confirmPasswordInput, { target: { value: 'SecureP@ssw0rd123' } });

    const submitButton = screen.getByRole('button', { name: /register|sign up/i });
    fireEvent.click(submitButton);

    await waitFor(() => {
      expect(submitButton).toBeDisabled();
    });
  });

  it('has link to login page', () => {
    renderWithProviders(<RegistrationForm />);

    const loginLink = screen.getByRole('link', { name: /login|sign in/i });
    expect(loginLink).toBeInTheDocument();
    expect(loginLink).toHaveAttribute('href', '/login');
  });
});
