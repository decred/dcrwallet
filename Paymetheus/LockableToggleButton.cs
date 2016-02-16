// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System.Windows;
using System.Windows.Controls.Primitives;

namespace Paymetheus
{
    public sealed class LockableToggleButton : ToggleButton
    {
        protected override void OnToggle()
        {
            if (!Locked)
            {
                base.OnToggle();
            }
        }

        public bool Locked
        {
            get { return (bool)GetValue(LockedProperty); }
            set { SetValue(LockedProperty, value); }
        }

        public static readonly DependencyProperty LockedProperty =
            DependencyProperty.Register(nameof(Locked), typeof(bool), typeof(LockableToggleButton));
    }
}
