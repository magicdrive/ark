<?php
trait LogsActivity {
    protected function logActivity(): void {}
}
class UserService {
    use LogsActivity;
    public function run(): void {}
}
