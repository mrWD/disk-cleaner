package core

// Risk — насколько безопасно чистить категорию.
type Risk int

const (
	// RiskSafe — пересоздаётся само, ничего не теряется.
	// Пример: DerivedData, кэш npm.
	RiskSafe Risk = iota

	// RiskCaution — потеряется состояние, которое придётся восстанавливать
	// руками или долго скачивать. Пример: симуляторы, эмуляторы Android.
	RiskCaution
)

// Category — группа объектов одного вида с объяснением для пользователя.
//
// Тексты лежат в переводах провайдера по ключам от ID:
//
//	<ID>.title — заголовок:                "Derived Data"
//	<ID>.what  — что это такое
//	<ID>.why   — почему это можно удалить
//	<ID>.after — что будет после удаления
type Category struct {
	ID   string // "xcode.derived_data": сначала провайдер, потом категория
	Risk Risk
}

// Методы ниже собирают ключи переводов, чтобы строка ".what"
// не повторялась по всему коду с риском опечатки.
func (c Category) TitleKey() string { return c.ID + ".title" }
func (c Category) WhatKey() string  { return c.ID + ".what" }
func (c Category) WhyKey() string   { return c.ID + ".why" }
func (c Category) AfterKey() string { return c.ID + ".after" }
