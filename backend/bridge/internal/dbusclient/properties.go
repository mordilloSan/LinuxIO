package dbusclient

import (
	"context"
	"fmt"

	godbus "github.com/godbus/dbus/v5"
)

func GetVariantProperty(ctx context.Context, obj godbus.BusObject, iface, property string) (godbus.Variant, error) {
	var variant godbus.Variant
	err := obj.CallWithContext(
		requireContext(ctx),
		PropertiesGet,
		0,
		iface,
		property,
	).Store(&variant)
	if err != nil {
		return godbus.Variant{}, err
	}
	return variant, nil
}

func GetProperty[T any](ctx context.Context, obj godbus.BusObject, iface, property string) (T, error) {
	var zero T

	variant, err := GetVariantProperty(ctx, obj, iface, property)
	if err != nil {
		return zero, err
	}

	value, ok := variant.Value().(T)
	if !ok {
		return zero, fmt.Errorf("%s.%s has type %T, want %T", iface, property, variant.Value(), zero)
	}
	return value, nil
}

func (i SystemInterface) GetVariantProperty(ctx context.Context, property string) (godbus.Variant, error) {
	var result godbus.Variant
	err := i.Use(ctx, func(ctx context.Context, _ *godbus.Conn, obj godbus.BusObject) error {
		var err error
		result, err = GetVariantProperty(ctx, obj, i.Name, property)
		return err
	})
	return result, err
}

func GetInterfaceProperty[T any](ctx context.Context, iface SystemInterface, property string) (T, error) {
	var result T
	err := iface.Use(ctx, func(ctx context.Context, _ *godbus.Conn, obj godbus.BusObject) error {
		var err error
		result, err = GetProperty[T](ctx, obj, iface.Name, property)
		return err
	})
	return result, err
}
