<?php
class BaseService {
    protected function validate(): void {}
}
class UserService extends BaseService {
    public function execute(): void {
        $this->validate();
    }
}
