import { useAuth } from '@/features/auth/useAuth'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import { Input } from '@/components/ui/input'
import { formatDate } from '@/lib/format'

export function ProfileSection() {
  const { user } = useAuth()

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base font-medium">Profile</CardTitle>
        <CardDescription>
          Your account information
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="space-y-2">
          <Label htmlFor="email">Email</Label>
          <Input
            id="email"
            type="email"
            value={user?.email || ''}
            disabled
            className="bg-muted"
          />
          <p className="text-xs text-muted-foreground">
            Your email address cannot be changed
          </p>
        </div>

        {user?.created_at && (
          <div className="space-y-2">
            <Label>Member Since</Label>
            <p className="text-sm">{formatDate(user.created_at)}</p>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
