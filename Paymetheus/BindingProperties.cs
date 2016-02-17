// Copyright (c) 2016 The btcsuite developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System.Windows;
using System.Windows.Data;
using System.Windows.Input;

namespace Paymetheus
{
    static class BindingProperties
    {
        public static readonly DependencyProperty UpdateSourceOnEnterProperty =
            DependencyProperty.RegisterAttached(nameof(UpdateSourceOnEnter), typeof(DependencyProperty),
                typeof(BindingProperties), new PropertyMetadata(null, UpdateSourceOnEnter));

        public static DependencyProperty GetUpdateSourceOnEnterProperty(DependencyObject instance)
        {
            return (DependencyProperty)instance.GetValue(UpdateSourceOnEnterProperty);
        }

        public static void SetUpdateSourceOnEnterProperty(DependencyObject instance, DependencyProperty value)
        {
            instance.SetValue(UpdateSourceOnEnterProperty, value);
        }

        private static void UpdateSourceOnEnter(DependencyObject dp, DependencyPropertyChangedEventArgs e)
        {
            var element = dp as UIElement;
            if (element == null)
                return;

            if (e.OldValue != null)
                element.PreviewKeyDown -= UpdateSourceOnEnter_PreviewKeyDown;
            if (e.NewValue != null)
                element.PreviewKeyDown += UpdateSourceOnEnter_PreviewKeyDown;
        }

        private static void UpdateSourceOnEnter_PreviewKeyDown(object sender, KeyEventArgs e)
        {
            if (e.Key == Key.Enter)
            {
                var element = sender as UIElement;
                if (element == null)
                    return;

                var property = GetUpdateSourceOnEnterProperty(element);
                if (property == null)
                    return;

                BindingOperations.GetBindingExpression(element, property)?.UpdateSource();
            }
        }
    }
}
