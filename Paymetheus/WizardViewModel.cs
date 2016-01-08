// Copyright (c) 2016 The btcsuite developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System;
using System.Collections.Generic;
using System.Linq;
using System.Text;
using System.Threading.Tasks;

namespace Paymetheus
{
    class WizardViewModel : ViewModelBase
    {
        public WizardViewModel(ShellViewModel shell) : base(shell) { }

        private WizardDialogViewModelBase _currentDialog;
        public WizardDialogViewModelBase CurrentDialog
        {
            get { return _currentDialog; }
            set { _currentDialog = value; RaisePropertyChanged(); }
        }
    }
}
