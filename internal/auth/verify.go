package auth

// Verify проверяет пароль без создания сессии — для подтверждения опасных
// действий (например, смены источника обновлений). Подчиняется той же
// блокировке после серии ошибок и тому же правилу «одна проверка с адреса
// за раз», что и вход: иначе это был бы обходной путь для подбора пароля.
func (a *Auth) Verify(client, password string) error {
	hash, err := a.beginCheck(client)
	if err != nil {
		return err
	}
	// bcrypt медленный намеренно — держать на нём мьютекс значит остановить
	// все запросы, проверяющие сессию.
	ok := compareHash([]byte(hash), []byte(password)) == nil
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.checking, client)
	if !ok {
		a.failLocked(client)
		return ErrWrong
	}
	delete(a.failures, client)
	return nil
}
