// Copyright (c) 2016 The btcsuite developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System;
using System.Collections.Generic;
using System.Linq;
using System.Text;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Controls.Primitives;
using System.Windows.Data;
using System.Windows.Documents;
using System.Windows.Input;
using System.Windows.Media;
using System.Windows.Media.Imaging;
using System.Windows.Navigation;
using System.Windows.Shapes;

namespace Paymetheus
{
    public class LockableToggleButton : ToggleButton
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

        public static readonly DependencyProperty LockedProperty = DependencyProperty.Register(nameof(Locked), typeof(bool), typeof(LockableToggleButton));
    }
}
