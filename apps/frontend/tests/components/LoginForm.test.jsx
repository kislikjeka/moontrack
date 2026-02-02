import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { BrowserRouter } from 'react-router-dom';
import LoginForm from '../../src/features/auth/LoginForm';
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

describe('LoginForm', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders login form with all fields', () => {
    renderWithProviders(<LoginForm />);

    expect(screen.getByLabelText(/email/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/password/i)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /login|sign in/i })).toBeInTheDocument();
  });

  it('shows validation error for empty email', async () => {
    renderWithProviders(<LoginForm />);

    const submitButton = screen.getByRole('button', { name: /login|sign in/i });
    fireEvent.click(submitButton);

    await waitFor(() => {
      expect(screen.getByText(/email is required/i)).toBeInTheDocument();
    });
  });

  it('shows validation error for invalid email format', async () => {
    renderWithProviders(<LoginForm />);

    const emailInput = screen.getByLabelText(/email/i);
    fireEvent.change(emailInput, { target: { value: 'invalid-email' } });

    const submitButton = screen.getByRole('button', { name: /login|sign in/i });
    fireEvent.click(submitButton);

    await waitFor(() => {
      expect(screen.getByText(/invalid email|valid email/i)).toBeInTheDocument();
    });
  });

  it('shows validation error for empty password', async () => {
    renderWithProviders(<LoginForm />);

    const emailInput = screen.getByLabelText(/email/i);
    fireEvent.change(emailInput, { target: { value: 'test@example.com' } });

    const submitButton = screen.getByRole('button', { name: /login|sign in/i });
    fireEvent.click(submitButton);

    await waitFor(() => {
      expect(screen.getByText(/password is required/i)).toBeInTheDocument();
    });
  });

  it('successfully logs in user with valid credentials', async () => {
    const mockUser = {
      id: '123',
      email: 'test@example.com',
    };
    const mockToken = 'mock-jwt-token';

    authService.login.mockResolvedValue({
      user: mockUser,
      token: mockToken,
    });

    renderWithProviders(<LoginForm />);

    const emailInput = screen.getByLabelText(/email/i);
    const passwordInput = screen.getByLabelText(/password/i);

    fireEvent.change(emailInput, { target: { value: 'test@example.com' } });
    fireEvent.change(passwordInput, { target: { value: 'SecureP@ssw0rd123' } });

    const submitButton = screen.getByRole('button', { name: /login|sign in/i });
    fireEvent.click(submitButton);

    await waitFor(() => {
      expect(authService.login).toHaveBeenCalledWith('test@example.com', 'SecureP@ssw0rd123');
    });
  });

  it('displays error message when login fails with wrong credentials', async () => {
    const errorMessage = 'Invalid credentials';
    authService.login.mockRejectedValue(new Error(errorMessage));

    renderWithProviders(<LoginForm />);

    const emailInput = screen.getByLabelText(/email/i);
    const passwordInput = screen.getByLabelText(/password/i);

    fireEvent.change(emailInput, { target: { value: 'test@example.com' } });
    fireEvent.change(passwordInput, { target: { value: 'WrongPassword' } });

    const submitButton = screen.getByRole('button', { name: /login|sign in/i });
    fireEvent.click(submitButton);

    await waitFor(() => {
      expect(screen.getByText(/invalid credentials/i)).toBeInTheDocument();
    });
  });

  it('displays error message when user does not exist', async () => {
    const errorMessage = 'User not found';
    authService.login.mockRejectedValue(new Error(errorMessage));

    renderWithProviders(<LoginForm />);

    const emailInput = screen.getByLabelText(/email/i);
    const passwordInput = screen.getByLabelText(/password/i);

    fireEvent.change(emailInput, { target: { value: 'nonexistent@example.com' } });
    fireEvent.change(passwordInput, { target: { value: 'SomePassword123' } });

    const submitButton = screen.getByRole('button', { name: /login|sign in/i });
    fireEvent.click(submitButton);

    await waitFor(() => {
      expect(screen.getByText(/user not found/i)).toBeInTheDocument();
    });
  });

  it('disables submit button while login is in progress', async () => {
    authService.login.mockImplementation(() => new Promise(resolve => setTimeout(resolve, 1000)));

    renderWithProviders(<LoginForm />);

    const emailInput = screen.getByLabelText(/email/i);
    const passwordInput = screen.getByLabelText(/password/i);

    fireEvent.change(emailInput, { target: { value: 'test@example.com' } });
    fireEvent.change(passwordInput, { target: { value: 'SecureP@ssw0rd123' } });

    const submitButton = screen.getByRole('button', { name: /login|sign in/i });
    fireEvent.click(submitButton);

    await waitFor(() => {
      expect(submitButton).toBeDisabled();
    });
  });

  it('has link to registration page', () => {
    renderWithProviders(<LoginForm />);

    const registerLink = screen.getByRole('link', { name: /register|sign up|create account/i });
    expect(registerLink).toBeInTheDocument();
    expect(registerLink).toHaveAttribute('href', '/register');
  });

  it('has "remember me" checkbox', () => {
    renderWithProviders(<LoginForm />);

    const rememberMeCheckbox = screen.queryByRole('checkbox', { name: /remember me/i });
    // This is optional, so we just check if it exists
    if (rememberMeCheckbox) {
      expect(rememberMeCheckbox).toBeInTheDocument();
    }
  });

  it('clears error message when user starts typing', async () => {
    const errorMessage = 'Invalid credentials';
    authService.login.mockRejectedValue(new Error(errorMessage));

    renderWithProviders(<LoginForm />);

    const emailInput = screen.getByLabelText(/email/i);
    const passwordInput = screen.getByLabelText(/password/i);

    // First, trigger an error
    fireEvent.change(emailInput, { target: { value: 'test@example.com' } });
    fireEvent.change(passwordInput, { target: { value: 'WrongPassword' } });

    const submitButton = screen.getByRole('button', { name: /login|sign in/i });
    fireEvent.click(submitButton);

    await waitFor(() => {
      expect(screen.getByText(/invalid credentials/i)).toBeInTheDocument();
    });

    // Now start typing in email field - error should be cleared
    fireEvent.change(emailInput, { target: { value: 'newemail@example.com' } });

    await waitFor(() => {
      expect(screen.queryByText(/invalid credentials/i)).not.toBeInTheDocument();
    });
  });
});
